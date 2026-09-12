package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/alehatsman/moongit/internal/ci"
	"github.com/alehatsman/moongit/internal/config"
	"github.com/alehatsman/moongit/internal/server"
	"github.com/alehatsman/moongit/internal/storage"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	args := os.Args[1:]
	if len(args) == 0 {
		if err := runServe(logger); err != nil {
			logger.Error("serve failed", "err", err)
			os.Exit(1)
		}
		return
	}

	switch args[0] {
	case "serve":
		if err := runServe(logger); err != nil {
			logger.Error("serve failed", "err", err)
			os.Exit(1)
		}
	case "repo":
		if err := runRepo(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "token":
		if err := runToken(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "ci":
		if err := runCIAdmin(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "help", "-h", "--help":
		printUsage(os.Stdout)
	default:
		fmt.Fprintln(os.Stderr, "unknown subcommand:", args[0])
		printUsage(os.Stderr)
		os.Exit(2)
	}
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `moongitd — moongit server daemon

USAGE:
    moongitd [serve]                       run the HTTP server (default)
    moongitd repo create <owner>/<name>    register a repo in the database
    moongitd token create <name>           mint a new API token (shown once)
    moongitd token list                    list all tokens (no plaintext)
    moongitd token revoke <name>           disable a token
    moongitd ci install-hooks              (re)install CI post-receive hooks
    moongitd help                          show this message

Environment:
    MOONGIT_ADDR        listen address (default ":8080")
    MOONGIT_DATA_DIR    data dir for SQLite + repos (default "data")
    MOONGIT_DB_PATH     SQLite path (default "$MOONGIT_DATA_DIR/moongit.db")
    MOONGIT_REPOS_DIR   bare repo root (default "$MOONGIT_DATA_DIR/repos")
    MOONGIT_WEB_DIR     built web UI dir (web/dist); empty serves API + git only
    MOONGIT_BASIC_USER  HTTP Basic user gating the web UI + git; empty disables it
    MOONGIT_BASIC_PASS  HTTP Basic password (paired with MOONGIT_BASIC_USER)
    MOONGIT_SSH_ADDR    listen address for the opt-in git SSH transport (e.g. ":2222"); empty disables SSH (one port)
    MOONGIT_SSH_HOST_KEY  SSH host key path (default "$MOONGIT_DATA_DIR/ssh_host_ed25519_key"); generated if absent
`)
}

func runServe(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if err := cfg.EnsureDirs(); err != nil {
		return fmt.Errorf("ensure dirs: %w", err)
	}

	db, err := storage.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("storage: %w", err)
	}
	defer db.Close()

	if err := storage.Migrate(db); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	// Read-only pool, opened after Migrate so the file is already in WAL.
	// Lets concurrent reads (a polling fleet of agents) bypass the writer.
	rdb, err := storage.OpenRead(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("storage (read): %w", err)
	}
	defer rdb.Close()

	n, err := storage.CountActiveTokens(db)
	if err != nil {
		return fmt.Errorf("count tokens: %w", err)
	}
	if n == 0 {
		logger.Warn("no active API tokens configured — all /api/* requests will be rejected. " +
			"Run `moongitd token create <name>` to bootstrap.")
	}

	srv := server.New(cfg, db, rdb, logger)
	// The in-process CI runner is created here (not inside its goroutine) so the
	// server can share it: it backs the agent force-stop endpoint (#146), holding
	// the in-memory turn handles needed to interrupt a live container — something
	// a DB-only signal can't do.
	runner := newCIRunner(db, cfg, logger)
	srv.SetAgentCanceler(runner)
	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go runClaimReaper(ctx, db, cfg.ClaimLease, logger)
	go runTokenReaper(ctx, db, cfg.AgentTokenTTL, logger)
	go runCIRetentionReaper(ctx, db, cfg, logger)
	go runEventRetentionReaper(ctx, db, cfg, logger)
	go runCronScheduler(ctx, db, cfg, logger)

	// The CI runner can be mid-run when shutdown fires. Cancelling its context
	// aborts the in-flight steps and it finalizes the run to a terminal status —
	// but those status writes need the DB still open, so we drain it (below)
	// before the deferred db.Close(). Otherwise a clean restart strands the run
	// 'running' (its terminal write hits a closed DB). ciCtx has its own cancel
	// (not just the signal's) so the runner also stops on the serve-error path,
	// where ctx is never cancelled. ReconcileOrphanRuns remains the crash safety
	// net for SIGKILL / power loss, where no drain runs.
	ciCtx, cancelCI := context.WithCancel(ctx)
	defer cancelCI()
	ciDone := make(chan struct{})
	go func() {
		defer close(ciDone)
		runCIRunner(ciCtx, runner)
	}()

	// Buffered for both listeners so neither blocks sending on shutdown (which
	// would otherwise wedge the SSH drain below).
	listenErr := make(chan error, 2)
	go func() {
		logger.Info("listening", "addr", cfg.Addr, "repos_dir", cfg.ReposDir)
		err := httpSrv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			listenErr <- err
			return
		}
		listenErr <- nil
	}()

	// Opt-in git SSH transport: a second listener on the same process, started
	// only when MOONGIT_SSH_ADDR is set so the default deployment stays one
	// port. It shuts down with ctx; a listen failure here surfaces on listenErr
	// to bring the whole process down rather than silently losing SSH. sshDone
	// closes when the listener has drained, so shutdown can wait for it before
	// the DB closes.
	var sshDone chan struct{}
	if cfg.SSHAddr != "" {
		sshDone = make(chan struct{})
		go func() {
			defer close(sshDone)
			if err := srv.ServeSSH(ctx, cfg.SSHAddr); err != nil {
				listenErr <- err
			}
		}()
	}

	var serveErr error
	select {
	case serveErr = <-listenErr:
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil && serveErr == nil {
		serveErr = err
	}
	// Stop the SSH listener and wait for it to drain before the deferred
	// db.Close(), so an in-flight git-over-SSH op can't touch a closed DB. The
	// SSH goroutine is bound to ctx (the signal context), which the signal path
	// already cancelled but the serve-error path has not — cancel it explicitly
	// via stop() so this holds on both paths.
	if sshDone != nil {
		stop()
		<-sshDone
	}
	// Cancel + drain the CI runner before the deferred db.Close() so an in-flight
	// run finalizes its status against an open DB. This runs on both exit paths,
	// including the serve-error path where ctx (signal-bound) is never cancelled.
	// The drain is intentionally unbounded: a wedged step hangs here rather than
	// racing db.Close() under a timeout — systemd's TimeoutStopSec then SIGKILLs
	// us, and ReconcileOrphanRuns cleans up on the next boot.
	cancelCI()
	<-ciDone
	return serveErr
}

// reaperFloor bounds how often the claim reaper runs, so a small lease (or a
// test) can't make it spin. The cadence is otherwise lease/2 — frequent
// enough that an orphaned claim surfaces as unassigned well within one lease.
const reaperFloor = time.Minute

// runClaimReaper periodically releases expired claims so orphaned work becomes
// discoverable, not just stealable. No-op (returns immediately) when expiry is
// disabled. Runs until ctx is cancelled, on the single writer pool.
func runClaimReaper(ctx context.Context, db *sql.DB, lease time.Duration, logger *slog.Logger) {
	if lease <= 0 {
		logger.Info("claim reaper disabled (lease <= 0)")
		return
	}
	interval := lease / 2
	if interval < reaperFloor {
		interval = reaperFloor
	}
	logger.Info("claim reaper started", "lease", lease, "interval", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := storage.ExpireClaims(db, lease)
			if err != nil {
				logger.Error("claim reaper", "err", err)
				continue
			}
			if n > 0 {
				logger.Info("claim reaper released expired claims", "count", n)
			}
		}
	}
}

// runTokenReaper periodically revokes idle per-agent session tokens
// (agent#<n>) so they don't accumulate unbounded — the `ce` launcher mints
// a fresh one per spawn and never cleans them up. No-op (returns
// immediately) when ttl <= 0. Runs until ctx is cancelled, on the single
// writer pool — same discipline as the claim reaper.
func runTokenReaper(ctx context.Context, db *sql.DB, ttl time.Duration, logger *slog.Logger) {
	if ttl <= 0 {
		logger.Info("token reaper disabled (ttl <= 0)")
		return
	}
	interval := ttl / 2
	if interval < reaperFloor {
		interval = reaperFloor
	}
	logger.Info("token reaper started", "ttl", ttl, "interval", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := storage.RevokeStaleAgentTokens(db, ttl)
			if err != nil {
				logger.Error("token reaper", "err", err)
				continue
			}
			if n > 0 {
				logger.Info("token reaper revoked stale agent tokens", "count", n)
			}
		}
	}
}

// ciRetentionInterval is how often the CI retention reaper sweeps. GC isn't
// time-critical — a fixed, unhurried cadence keeps disk + DB bounded without
// another tuning knob.
const ciRetentionInterval = 10 * time.Minute

// runCIRetentionReaper periodically enforces per-repo CI run retention: it
// prunes terminal runs beyond the newest cfg.CIRetainRuns (rows via
// storage.PruneRuns) and deletes their on-disk event logs, so neither the DB
// nor data/ci grows without bound. It sweeps once at startup, then on a fixed
// interval. No-op (returns immediately) when retention is disabled. Runs until
// ctx is cancelled, on the single writer pool — same discipline as the claim
// and token reapers.
func runCIRetentionReaper(ctx context.Context, db *sql.DB, cfg *config.Config, logger *slog.Logger) {
	if cfg.CIRetainRuns <= 0 {
		logger.Info("ci retention reaper disabled (retain <= 0)")
		return
	}
	logger.Info("ci retention reaper started", "retain", cfg.CIRetainRuns, "interval", ciRetentionInterval)

	sweepCIRetention(db, cfg, logger) // initial pass so a restart promptly clears any backlog

	ticker := time.NewTicker(ciRetentionInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepCIRetention(db, cfg, logger)
		}
	}
}

// sweepCIRetention runs one retention pass: prune terminal runs beyond the
// retain window (rows) and delete each pruned run's on-disk event-log dir.
// Disk errors are logged, not fatal — the rows are already gone, so a leftover
// dir is harmless and retried implicitly (it won't be re-listed once the row
// is deleted, but RemoveAll on an absent path is a no-op anyway).
func sweepCIRetention(db *sql.DB, cfg *config.Config, logger *slog.Logger) {
	pruned, err := storage.PruneRuns(db, cfg.CIRetainRuns)
	if err != nil {
		logger.Error("ci retention prune", "err", err)
		return
	}
	for _, p := range pruned {
		dir := ci.RunLogDir(cfg.DataDir, p.Owner, p.Repo, p.Number)
		if err := os.RemoveAll(dir); err != nil {
			logger.Error("ci retention rm logs", "dir", dir, "err", err)
		}
	}
	if len(pruned) > 0 {
		logger.Info("ci retention pruned runs", "count", len(pruned))
	}
}

// runEventRetentionReaper periodically bounds the outbound event feed (#73) to
// the newest cfg.EventRetain rows via storage.PruneEvents. It sweeps once at
// startup, then on the same unhurried cadence as CI retention. No-op when
// retention is disabled. Runs until ctx is cancelled, on the single writer pool
// — same discipline as the other reapers.
func runEventRetentionReaper(ctx context.Context, db *sql.DB, cfg *config.Config, logger *slog.Logger) {
	if cfg.EventRetain <= 0 {
		logger.Info("event retention reaper disabled (retain <= 0)")
		return
	}
	logger.Info("event retention reaper started", "retain", cfg.EventRetain, "interval", ciRetentionInterval)

	sweepEventRetention(db, cfg, logger) // initial pass so a restart promptly clears any backlog

	ticker := time.NewTicker(ciRetentionInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepEventRetention(db, cfg, logger)
		}
	}
}

// sweepEventRetention runs one event-feed retention pass.
func sweepEventRetention(db *sql.DB, cfg *config.Config, logger *slog.Logger) {
	n, err := storage.PruneEvents(db, cfg.EventRetain)
	if err != nil {
		logger.Error("event retention prune", "err", err)
		return
	}
	if n > 0 {
		logger.Info("event retention pruned events", "count", n)
	}
}

func runRepo(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: moongitd repo <create>")
	}
	switch args[0] {
	case "create":
		return runRepoCreate(args[1:])
	default:
		return fmt.Errorf("unknown repo subcommand: %s", args[0])
	}
}

func runCIAdmin(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: moongitd ci install-hooks")
	}
	switch args[0] {
	case "install-hooks":
		return runCIInstallHooks(args[1:])
	default:
		return fmt.Errorf("unknown ci subcommand: %s", args[0])
	}
}

// runCIInstallHooks backfills the CI post-receive hook into every registered
// repo's bare directory — for repos created before the hook existed.
func runCIInstallHooks(args []string) error {
	if len(args) != 0 {
		return errors.New("usage: moongitd ci install-hooks")
	}
	cfg, db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	repos, err := storage.ListRepos(db)
	if err != nil {
		return err
	}
	installed := 0
	for _, repo := range repos {
		bare := filepath.Join(cfg.ReposDir, repo.Owner, repo.Name+".git")
		if _, err := os.Stat(bare); err != nil {
			fmt.Fprintf(os.Stderr, "skip %s/%s: %v\n", repo.Owner, repo.Name, err)
			continue
		}
		if err := server.WritePostReceiveHook(bare); err != nil {
			return fmt.Errorf("%s/%s: %w", repo.Owner, repo.Name, err)
		}
		fmt.Printf("installed hook: %s/%s\n", repo.Owner, repo.Name)
		installed++
	}
	fmt.Printf("installed %d hook(s)\n", installed)
	return nil
}

func runToken(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: moongitd token <create|list|revoke>")
	}
	switch args[0] {
	case "create":
		return runTokenCreate(args[1:])
	case "list":
		return runTokenList(args[1:])
	case "revoke":
		return runTokenRevoke(args[1:])
	default:
		return fmt.Errorf("unknown token subcommand: %s", args[0])
	}
}

func runTokenCreate(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: moongitd token create <name>")
	}
	_, db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	plaintext, err := storage.GenerateTokenString()
	if err != nil {
		return fmt.Errorf("generate token: %w", err)
	}
	tok, err := storage.CreateToken(db, args[0], plaintext)
	if err != nil {
		return fmt.Errorf("create token: %w", err)
	}
	fmt.Printf("created token %q (id=%d)\n", tok.Name, tok.ID)
	fmt.Println()
	fmt.Println("  " + plaintext)
	fmt.Println()
	fmt.Println("Save this token now — it will NOT be shown again.")
	fmt.Println("Clients should set MOONGIT_TOKEN to use it.")
	return nil
}

func runTokenList(args []string) error {
	if len(args) != 0 {
		return errors.New("usage: moongitd token list")
	}
	_, db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	tokens, err := storage.ListTokens(db)
	if err != nil {
		return err
	}
	if len(tokens) == 0 {
		fmt.Println("(no tokens)")
		return nil
	}
	fmt.Printf("%-4s  %-20s  %-25s  %-25s  %s\n", "ID", "NAME", "CREATED", "LAST_USED", "STATUS")
	for _, t := range tokens {
		status := "active"
		if t.RevokedAt != nil {
			status = "revoked"
		}
		lastUsed := "(never)"
		if t.LastUsedAt != nil {
			lastUsed = t.LastUsedAt.Local().Format(time.RFC3339)
		}
		fmt.Printf("%-4d  %-20s  %-25s  %-25s  %s\n",
			t.ID, t.Name, t.CreatedAt.Local().Format(time.RFC3339), lastUsed, status)
	}
	return nil
}

func runTokenRevoke(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: moongitd token revoke <name>")
	}
	_, db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()
	if err := storage.RevokeToken(db, args[0]); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return fmt.Errorf("token %q not found", args[0])
		}
		return fmt.Errorf("revoke: %w", err)
	}
	fmt.Printf("revoked token %q\n", args[0])
	return nil
}

// openDB centralizes the load/ensure/open/migrate sequence used by CLI
// subcommands that need direct DB access (token, repo). Returns the
// loaded config too so callers can resolve paths like ReposDir.
func openDB() (*config.Config, *sql.DB, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, fmt.Errorf("config: %w", err)
	}
	if err := cfg.EnsureDirs(); err != nil {
		return nil, nil, fmt.Errorf("ensure dirs: %w", err)
	}
	db, err := storage.Open(cfg.DBPath)
	if err != nil {
		return nil, nil, fmt.Errorf("storage: %w", err)
	}
	if err := storage.Migrate(db); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("migrate: %w", err)
	}
	return cfg, db, nil
}

func runRepoCreate(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: moongitd repo create <owner>/<name>")
	}
	owner, name, ok := strings.Cut(args[0], "/")
	if !ok || owner == "" || name == "" {
		return fmt.Errorf("invalid repo spec %q (want owner/name)", args[0])
	}
	name = strings.TrimSuffix(name, ".git")

	cfg, db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	id, repoDir, err := server.CreateRepo(db, cfg.ReposDir, owner, name)
	if err != nil {
		return err
	}
	fmt.Printf("repo registered: %s/%s (id=%d) at %s\n", owner, name, id, repoDir)
	return nil
}
