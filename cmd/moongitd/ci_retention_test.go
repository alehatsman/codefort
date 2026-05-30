package main

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/alehatsman/moongit/internal/ci"
	"github.com/alehatsman/moongit/internal/config"
	"github.com/alehatsman/moongit/internal/storage"
)

// TestSweepCIRetentionDeletesRowsAndLogs is the on-disk half of #59: a sweep
// must prune both the DB rows and the event-log dirs of runs beyond the retain
// window, leaving the newest ones untouched.
func TestSweepCIRetentionDeletesRowsAndLogs(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.Open(filepath.Join(dir, "ci.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	repoID, err := storage.EnsureRepo(db, "alice", "repo")
	if err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}

	// Four finished runs, each with an on-disk event log.
	for i := 1; i <= 4; i++ {
		run, err := storage.EnqueueRun(db, repoID, storage.NewRun{CommitSHA: "sha", Ref: "r", Event: "push"})
		if err != nil {
			t.Fatalf("EnqueueRun: %v", err)
		}
		if err := storage.FinishRun(db, run.ID, storage.RunSuccess); err != nil {
			t.Fatalf("FinishRun: %v", err)
		}
		logPath := ci.EventLogPath(dir, "alice", "repo", run.Number, "build")
		if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
			t.Fatalf("mkdir log: %v", err)
		}
		if err := os.WriteFile(logPath, []byte(`{"type":"run.completed"}`+"\n"), 0o644); err != nil {
			t.Fatalf("write log: %v", err)
		}
	}

	cfg := &config.Config{DataDir: dir, CIRetainRuns: 2}
	sweepCIRetention(db, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// #1, #2 pruned: rows gone and log dirs gone.
	for _, n := range []int{1, 2} {
		if _, err := storage.GetRun(db, repoID, n); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("run #%d row still present (err=%v), want pruned", n, err)
		}
		if _, err := os.Stat(ci.RunLogDir(dir, "alice", "repo", n)); !os.IsNotExist(err) {
			t.Errorf("run #%d log dir still present, want removed", n)
		}
	}
	// #3, #4 retained: rows and log dirs intact.
	for _, n := range []int{3, 4} {
		if _, err := storage.GetRun(db, repoID, n); err != nil {
			t.Errorf("run #%d should be retained: %v", n, err)
		}
		if _, err := os.Stat(ci.RunLogDir(dir, "alice", "repo", n)); err != nil {
			t.Errorf("run #%d log dir should be retained: %v", n, err)
		}
	}
}
