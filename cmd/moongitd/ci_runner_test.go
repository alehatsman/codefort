package main

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alehatsman/moongit/internal/ci"
	"github.com/alehatsman/moongit/internal/config"
	"github.com/alehatsman/moongit/internal/storage"
)

// newTestRunner builds a runner over a migrated temp DB with one repo and one
// queued run, injecting fake checkout/pipeline/exec boundaries so no real git
// or mooncake is involved.
func newTestRunner(t *testing.T, pipeline string, enabled bool, exec stepExecutor) (*ciRunner, storage.CIRun) {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.Open(filepath.Join(dir, "ci.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	repoID, err := storage.EnsureRepo(db, "alice", "repo")
	if err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}
	if enabled {
		if err := storage.SetRepoCIEnabled(db, repoID, true); err != nil {
			t.Fatalf("SetRepoCIEnabled: %v", err)
		}
	}
	run, err := storage.EnqueueRun(db, repoID, storage.NewRun{
		CommitSHA: "deadbeefcafe", Ref: "refs/heads/main", Event: "push",
	})
	if err != nil {
		t.Fatalf("EnqueueRun: %v", err)
	}

	r := &ciRunner{
		db: db,
		cfg: &config.Config{
			DataDir:        dir,
			ReposDir:       filepath.Join(dir, "repos"),
			CIRunTimeout:   time.Minute,
			CIPollInterval: time.Second,
		},
		logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		exec:     exec,
		checkout: func(context.Context, string, string, string) error { return nil },
		readPipeline: func(string, string) ([]byte, bool, error) {
			if pipeline == "" {
				return nil, false, nil
			}
			return []byte(pipeline), true, nil
		},
	}
	return r, run
}

const failSentinel = "FAIL_HERE"

func successExec(context.Context, string, string) (stepResult, error) {
	return stepResult{RC: 0, Stdout: "ok\n"}, nil
}

func sentinelExec(_ context.Context, _ string, stepYAML string) (stepResult, error) {
	if strings.Contains(stepYAML, failSentinel) {
		return stepResult{RC: 1, Failed: true, Stderr: "boom\n"}, nil
	}
	return stepResult{RC: 0, Stdout: "ok\n"}, nil
}

func TestExecuteRunSuccess(t *testing.T) {
	pipeline := `
version: "1"
jobs:
  build:
    steps: [{run: echo build}]
  test:
    needs: [build]
    steps: [{run: echo test}]
`
	r, run := newTestRunner(t, pipeline, true, successExec)
	r.executeRun(context.Background(), run)

	got, err := storage.GetRun(r.db, run.RepoID, run.Number)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Status != storage.RunSuccess {
		t.Errorf("run status = %q, want success", got.Status)
	}
	jobs, _ := storage.ListJobs(r.db, run.ID)
	if len(jobs) != 2 {
		t.Fatalf("jobs = %d, want 2", len(jobs))
	}
	for _, j := range jobs {
		if j.Status != storage.JobSuccess {
			t.Errorf("job %q = %q, want success", j.Name, j.Status)
		}
	}

	// The event stream for a job must carry the lifecycle + a stdout line.
	events, _, err := ci.ReadEvents(ci.EventLogPath(r.cfg.DataDir, "alice", "repo", run.Number, "build"), 0)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	types := map[string]bool{}
	var sawStdout bool
	for _, e := range events {
		types[e.Type] = true
		if e.Type == ci.EventStepStdout && e.Data["line"] == "ok" {
			sawStdout = true
		}
	}
	for _, want := range []string{ci.EventRunStarted, ci.EventStepStarted, ci.EventStepCompleted, ci.EventRunCompleted} {
		if !types[want] {
			t.Errorf("event stream missing %q (got %v)", want, types)
		}
	}
	if !sawStdout {
		t.Error("event stream missing step.stdout line 'ok'")
	}
}

func TestExecuteRunFailurePropagatesAndSkips(t *testing.T) {
	pipeline := `
version: "1"
jobs:
  build:
    steps: [{run: echo build}]
  test:
    needs: [build]
    steps: [{run: echo test}]
  deploy:
    needs: [test]
    steps: [{run: ` + failSentinel + `}]
  notify:
    needs: [deploy]
    steps: [{run: echo notify}]
`
	r, run := newTestRunner(t, pipeline, true, sentinelExec)
	r.executeRun(context.Background(), run)

	got, _ := storage.GetRun(r.db, run.RepoID, run.Number)
	if got.Status != storage.RunFailed {
		t.Errorf("run status = %q, want failed", got.Status)
	}

	jobs, _ := storage.ListJobs(r.db, run.ID)
	byName := map[string]storage.CIJob{}
	for _, j := range jobs {
		byName[j.Name] = j
	}
	want := map[string]storage.JobStatus{
		"build":  storage.JobSuccess,
		"test":   storage.JobSuccess,
		"deploy": storage.JobFailed,
		"notify": storage.JobSkipped,
	}
	for name, st := range want {
		if byName[name].Status != st {
			t.Errorf("job %q = %q, want %q", name, byName[name].Status, st)
		}
	}
	// The failed job records its exit code; the skipped job has none.
	if byName["deploy"].ExitCode == nil || *byName["deploy"].ExitCode != 1 {
		t.Errorf("deploy exit code = %v, want 1", byName["deploy"].ExitCode)
	}
	if byName["notify"].ExitCode != nil {
		t.Errorf("notify exit code = %v, want nil (skipped)", byName["notify"].ExitCode)
	}

	// The failed job's stream ends with run.failed.
	events, _, _ := ci.ReadEvents(ci.EventLogPath(r.cfg.DataDir, "alice", "repo", run.Number, "deploy"), 0)
	var sawFailed bool
	for _, e := range events {
		if e.Type == ci.EventRunFailed {
			sawFailed = true
		}
	}
	if !sawFailed {
		t.Error("deploy stream missing run.failed")
	}
}

func TestExecuteRunGatedWhenDisabled(t *testing.T) {
	pipeline := `
version: "1"
jobs:
  build:
    steps: [{run: echo build}]
`
	r, run := newTestRunner(t, pipeline, false /* ci disabled */, successExec)
	r.executeRun(context.Background(), run)

	got, _ := storage.GetRun(r.db, run.RepoID, run.Number)
	if got.Status != storage.RunCanceled {
		t.Errorf("run status = %q, want canceled (gated)", got.Status)
	}
	if jobs, _ := storage.ListJobs(r.db, run.ID); len(jobs) != 0 {
		t.Errorf("gated run created %d jobs, want 0", len(jobs))
	}
}

func TestExecuteRunGatedWhenNoPipeline(t *testing.T) {
	// Enabled, but readPipeline returns absent.
	r, run := newTestRunner(t, "" /* no mgitci.yml */, true, successExec)
	r.executeRun(context.Background(), run)

	got, _ := storage.GetRun(r.db, run.RepoID, run.Number)
	if got.Status != storage.RunCanceled {
		t.Errorf("run status = %q, want canceled (no pipeline)", got.Status)
	}
}
