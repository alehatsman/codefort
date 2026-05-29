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
	"strings"
	"syscall"
	"time"

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
    moongitd help                          show this message

Environment:
    MOONGIT_ADDR        listen address (default ":8080")
    MOONGIT_DATA_DIR    data dir for SQLite + repos (default "data")
    MOONGIT_DB_PATH     SQLite path (default "$MOONGIT_DATA_DIR/moongit.db")
    MOONGIT_REPOS_DIR   bare repo root (default "$MOONGIT_DATA_DIR/repos")
    MOONGIT_DEX_URL     dex serve base URL for the Intel tab (e.g. http://127.0.0.1:8080; empty disables it)
    MOONGIT_DEX_TOKEN   bearer token for dex (DEX_SERVE_TOKEN); empty for token-less loopback
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
	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go runClaimReaper(ctx, db, cfg.ClaimLease, logger)

	listenErr := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", cfg.Addr, "repos_dir", cfg.ReposDir)
		err := httpSrv.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			listenErr <- err
			return
		}
		listenErr <- nil
	}()

	select {
	case err := <-listenErr:
		return err
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpSrv.Shutdown(shutdownCtx)
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
