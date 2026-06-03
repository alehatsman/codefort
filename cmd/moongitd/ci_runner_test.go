package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync/atomic"
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
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		newSession: func(_ context.Context, _, workDir, _ string) (jobSession, error) {
			return &hostSession{workDir: workDir, exec: exec}, nil
		},
		checkout: func(context.Context, string, string, string) error { return nil },
		readPipeline: func(context.Context, string, string) ([]byte, bool, error) {
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

// trackingExec returns a step executor that records the peak number of
// concurrent in-flight executions, holding each for hold so any overlap is
// observable. With one step per job, the peak doubles as the peak number of
// concurrently executing runs.
func trackingExec(hold time.Duration) (stepExecutor, *int64) {
	var cur, maxSeen int64
	exec := func(context.Context, string, string) (stepResult, error) {
		n := atomic.AddInt64(&cur, 1)
		for {
			old := atomic.LoadInt64(&maxSeen)
			if n <= old || atomic.CompareAndSwapInt64(&maxSeen, old, n) {
				break
			}
		}
		time.Sleep(hold)
		atomic.AddInt64(&cur, -1)
		return stepResult{RC: 0, Stdout: "ok\n"}, nil
	}
	return exec, &maxSeen
}

// enqueueExtraRuns queues n additional runs on repoID (beyond the one
// newTestRunner already seeded), so the runner has a backlog to dispatch.
func enqueueExtraRuns(t *testing.T, r *ciRunner, repoID int64, n int) {
	t.Helper()
	for i := range n {
		if _, err := storage.EnqueueRun(r.db, repoID, storage.NewRun{
			CommitSHA: fmt.Sprintf("deadbeef%04d", i), Ref: "refs/heads/main", Event: "push",
		}); err != nil {
			t.Fatalf("EnqueueRun: %v", err)
		}
	}
}

// waitRunsTerminal blocks until every numbered run on repoID reaches a terminal
// status, or fails the test on timeout.
func waitRunsTerminal(t *testing.T, r *ciRunner, repoID int64, numbers []int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		done := 0
		for _, num := range numbers {
			got, err := storage.GetRun(r.db, repoID, num)
			if err != nil {
				t.Fatalf("GetRun %d: %v", num, err)
			}
			if got.Status != storage.RunQueued && got.Status != storage.RunRunning {
				done++
			}
		}
		if done == len(numbers) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d/%d runs terminal before timeout", done, len(numbers))
		}
		time.Sleep(5 * time.Millisecond)
	}
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

func TestExecuteRunParallelRoots(t *testing.T) {
	// a and b are independent roots; c joins them. With concurrency > 1 the two
	// roots must overlap in time.
	pipeline := `
version: "1"
jobs:
  a:
    steps: [{run: echo a}]
  b:
    steps: [{run: echo b}]
  c:
    needs: [a, b]
    steps: [{run: echo c}]
`
	trackExec, maxSeen := trackingExec(40 * time.Millisecond) // hold widens the overlap window

	r, run := newTestRunner(t, pipeline, true, trackExec)
	r.cfg.CIJobConcurrency = 4
	r.executeRun(context.Background(), run)

	got, _ := storage.GetRun(r.db, run.RepoID, run.Number)
	if got.Status != storage.RunSuccess {
		t.Errorf("run status = %q, want success", got.Status)
	}
	jobs, _ := storage.ListJobs(r.db, run.ID)
	if len(jobs) != 3 {
		t.Fatalf("jobs = %d, want 3", len(jobs))
	}
	for _, j := range jobs {
		if j.Status != storage.JobSuccess {
			t.Errorf("job %q = %q, want success", j.Name, j.Status)
		}
	}
	if peak := atomic.LoadInt64(maxSeen); peak < 2 {
		t.Errorf("max concurrent steps = %d, want >= 2 (roots a and b should overlap)", peak)
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

// TestRunFinalizesInFlightOnCancel guards the runner-side half of the shutdown
// drain (#57): when ctx is cancelled while a run is mid-step, run() must let
// executeRun finalize the run to a terminal status and then return — never
// leave it stranded 'running'. The other half — runServe ordering db.Close()
// after this drain — lives in main.go and isn't exercised here.
func TestRunFinalizesInFlightOnCancel(t *testing.T) {
	pipeline := `
version: "1"
jobs:
  build:
    steps: [{run: echo build}]
`
	started := make(chan struct{})
	// Mimic a real step that honors ctx: it blocks until shutdown cancels the
	// run, then reports the cancellation like parseStepResult does.
	blockingExec := func(ctx context.Context, _, _ string) (stepResult, error) {
		close(started)
		<-ctx.Done()
		return stepResult{}, ctx.Err()
	}
	r, run := newTestRunner(t, pipeline, true, blockingExec)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		r.run(ctx)
		close(done)
	}()

	<-started // runner has claimed the run and is mid-step
	cancel()  // simulate the shutdown signal

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("run() did not return after ctx cancel — shutdown would hang")
	}

	// The drain must have finalized the run against the open DB — and as
	// interrupted, not error: a shutdown-cancelled run is operator-induced
	// (deploy/restart), not a gate failure (#143).
	got, err := storage.GetRun(r.db, run.RepoID, run.Number)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Status != storage.RunInterrupted {
		t.Errorf("run status = %q after shutdown, want interrupted", got.Status)
	}
	if got.FinishedAt == nil {
		t.Error("run has no FinishedAt after shutdown drain")
	}
	jobs, _ := storage.ListJobs(r.db, run.ID)
	for _, j := range jobs {
		if j.Status != storage.JobInterrupted {
			t.Errorf("job %q = %q after shutdown, want interrupted", j.Name, j.Status)
		}
	}
}

// TestRunTimeoutFinalizesError is the discriminator's other half (#143): a run
// whose own CIRunTimeout fires (ctx.Err() == DeadlineExceeded) is a genuine
// infrastructure failure and must finalize error — NOT interrupted, which is
// reserved for a shutdown-cancelled context. The two share the "ctx.Err() != nil
// while mid-step" code path, so this guards that they don't collapse together.
func TestRunTimeoutFinalizesError(t *testing.T) {
	pipeline := `
version: "1"
jobs:
  build:
    steps: [{run: echo build}]
`
	// A step that honors ctx and reports its cancellation, like parseStepResult.
	blockingExec := func(ctx context.Context, _, _ string) (stepResult, error) {
		<-ctx.Done()
		return stepResult{}, ctx.Err()
	}
	r, run := newTestRunner(t, pipeline, true, blockingExec)
	r.cfg.CIRunTimeout = 20 * time.Millisecond // fire the run timeout, not a shutdown

	r.executeRun(context.Background(), run) // parent never cancelled — only the timeout fires

	got, err := storage.GetRun(r.db, run.RepoID, run.Number)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Status != storage.RunError {
		t.Errorf("run status = %q after run-timeout, want error", got.Status)
	}
	jobs, _ := storage.ListJobs(r.db, run.ID)
	for _, j := range jobs {
		if j.Status != storage.JobError {
			t.Errorf("job %q = %q after run-timeout, want error", j.Name, j.Status)
		}
	}
}

// TestCancelCIRunInterruptsInFlight is the operator force-stop (#296): a running
// CI run, when CancelCIRun fires, unwinds its job loop and finalizes RunCanceled
// — distinct from the RunInterrupted a shutdown produces over the same
// ctx-cancelled path.
func TestCancelCIRunInterruptsInFlight(t *testing.T) {
	pipeline := `
version: "1"
jobs:
  build:
    steps: [{run: echo build}]
`
	started := make(chan struct{})
	blockingExec := func(ctx context.Context, _, _ string) (stepResult, error) {
		close(started)
		<-ctx.Done()
		return stepResult{}, ctx.Err()
	}
	r, run := newTestRunner(t, pipeline, true, blockingExec)

	done := make(chan struct{})
	go func() {
		r.executeRun(context.Background(), run)
		close(done)
	}()

	<-started // run is claimed and mid-step; its cancel handle is registered
	if _, ok := r.runCancels.Load(run.ID); !ok {
		t.Fatal("running CI run did not register a cancel handle")
	}
	if !r.CancelCIRun(run.ID) {
		t.Fatal("CancelCIRun returned false for a running run")
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("executeRun did not return after cancel — jobs not interrupted")
	}

	got, err := storage.GetRun(r.db, run.RepoID, run.Number)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Status != storage.RunCanceled {
		t.Errorf("run status = %q after operator stop, want canceled (not interrupted)", got.Status)
	}
	if got.FinishedAt == nil {
		t.Error("canceled run has no FinishedAt")
	}
	jobs, _ := storage.ListJobs(r.db, run.ID)
	for _, j := range jobs {
		if j.Status != storage.JobInterrupted {
			t.Errorf("job %q = %q after cancel, want interrupted", j.Name, j.Status)
		}
	}
}

// TestCancelCIRunQueued: a run still queued (no goroutine yet) is CAS'd straight
// to canceled at the storage layer, and a second cancel no-ops (already
// terminal).
func TestCancelCIRunQueued(t *testing.T) {
	pipeline := `
version: "1"
jobs:
  build:
    steps: [{run: echo build}]
`
	r, run := newTestRunner(t, pipeline, true, successExec) // seeded run, never executed

	if !r.CancelCIRun(run.ID) {
		t.Fatal("CancelCIRun returned false for a queued run")
	}
	got, err := storage.GetRun(r.db, run.RepoID, run.Number)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Status != storage.RunCanceled {
		t.Errorf("run status = %q, want canceled", got.Status)
	}
	if r.CancelCIRun(run.ID) {
		t.Error("CancelCIRun returned true for an already-canceled run")
	}
}

// TestRunDispatchesConcurrentRunsWithinCap verifies the shared budget caps how
// many runs execute at once: with a backlog of 5 runs and MaxConcurrency 3, the
// runs must overlap (peak >= 2) yet never exceed the cap (peak <= 3).
func TestRunDispatchesConcurrentRunsWithinCap(t *testing.T) {
	pipeline := `
version: "1"
jobs:
  build:
    steps: [{run: echo build}]
`
	exec, maxSeen := trackingExec(40 * time.Millisecond)
	r, run := newTestRunner(t, pipeline, true, exec)
	r.cfg.MaxConcurrency = 3
	enqueueExtraRuns(t, r, run.RepoID, 4) // 5 runs total: numbers 1..5

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		r.run(ctx)
		close(done)
	}()

	waitRunsTerminal(t, r, run.RepoID, []int{1, 2, 3, 4, 5}, 5*time.Second)
	cancel()
	<-done

	peak := atomic.LoadInt64(maxSeen)
	if peak > 3 {
		t.Errorf("peak concurrent runs = %d, want <= 3 (cap not honored)", peak)
	}
	if peak < 2 {
		t.Errorf("peak concurrent runs = %d, want >= 2 (runs should overlap)", peak)
	}
}

// TestRunDefaultRunConcurrencyIsSequential pins the default: with
// MaxConcurrency unset (0 -> treated as 1), runs execute strictly one at a
// time, preserving the historical single-worker behavior.
func TestRunDefaultRunConcurrencyIsSequential(t *testing.T) {
	pipeline := `
version: "1"
jobs:
  build:
    steps: [{run: echo build}]
`
	exec, maxSeen := trackingExec(20 * time.Millisecond)
	r, run := newTestRunner(t, pipeline, true, exec) // MaxConcurrency left 0 -> 1
	enqueueExtraRuns(t, r, run.RepoID, 3)            // 4 runs total: numbers 1..4

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		r.run(ctx)
		close(done)
	}()

	waitRunsTerminal(t, r, run.RepoID, []int{1, 2, 3, 4}, 5*time.Second)
	cancel()
	<-done

	if peak := atomic.LoadInt64(maxSeen); peak != 1 {
		t.Errorf("peak concurrent runs = %d with default concurrency, want 1 (sequential)", peak)
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

func TestContainerName(t *testing.T) {
	// jobID makes the name collision-free; the job name is sanitized to
	// docker's [a-zA-Z0-9_.-] charset and the prefix stays a valid leading char.
	got := containerName(42, "build/test step")
	if want := "moongit-ci-42-build-test-step"; got != want {
		t.Errorf("containerName = %q, want %q", got, want)
	}
	if got := sanitizeContainerName("ok_.-9AZ"); got != "ok_.-9AZ" {
		t.Errorf("sanitizeContainerName mangled a valid name: %q", got)
	}
}

func TestParseStepResult(t *testing.T) {
	// mooncake prints JSON to stdout even when the step fails and exits
	// non-zero; parseStepResult must trust stdout, not the process error.
	stdout := []byte(`{"rc":3,"failed":true,"stdout":"boom\n"}`)
	res, err := parseStepResult(context.Background(), stdout, nil, errors.New("exit status 3"))
	if err != nil {
		t.Fatalf("parseStepResult: %v", err)
	}
	if res.RC != 3 || !res.Failed || res.Stdout != "boom\n" {
		t.Errorf("got %+v, want rc=3 failed=true stdout=boom", res)
	}

	// Unparseable stdout surfaces as an executor error (with stderr context).
	if _, err := parseStepResult(context.Background(), []byte("not json"), []byte("kaboom"), errors.New("x")); err == nil {
		t.Error("parseStepResult accepted non-JSON stdout, want error")
	}

	// A cancelled context is an executor error regardless of output.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := parseStepResult(ctx, stdout, nil, nil); err == nil {
		t.Error("parseStepResult ignored a cancelled context, want error")
	}
}
