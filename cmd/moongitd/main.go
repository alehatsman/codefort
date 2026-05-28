package main

import (
	"context"
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
    moongitd help                          show this message

Environment:
    MOONGIT_ADDR        listen address (default ":8080")
    MOONGIT_DATA_DIR    data dir for SQLite + repos (default "data")
    MOONGIT_DB_PATH     SQLite path (default "$MOONGIT_DATA_DIR/moongit.db")
    MOONGIT_REPOS_DIR   bare repo root (default "$MOONGIT_DATA_DIR/repos")
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

	srv := server.New(cfg, db, logger)
	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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

func runRepoCreate(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: moongitd repo create <owner>/<name>")
	}
	owner, name, ok := strings.Cut(args[0], "/")
	if !ok || owner == "" || name == "" {
		return fmt.Errorf("invalid repo spec %q (want owner/name)", args[0])
	}
	name = strings.TrimSuffix(name, ".git")

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

	id, err := storage.EnsureRepo(db, owner, name)
	if err != nil {
		return fmt.Errorf("ensure repo: %w", err)
	}
	fmt.Printf("repo registered: %s/%s (id=%d)\n", owner, name, id)
	return nil
}
