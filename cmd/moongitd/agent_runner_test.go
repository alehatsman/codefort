package main

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alehatsman/moongit/internal/api"
	"github.com/alehatsman/moongit/internal/ci"
	"github.com/alehatsman/moongit/internal/config"
	"github.com/alehatsman/moongit/internal/storage"
)

// fakeAgentSession is a jobSession whose steps are never executed — the #74
// seam only opens/closes the container and emits placeholder events.
type fakeAgentSession struct{ closed *atomic.Int32 }

func (f *fakeAgentSession) Exec(context.Context, string) (stepResult, error) {
	return stepResult{}, nil
}
func (f *fakeAgentSession) Close() error { f.closed.Add(1); return nil }

// newAgentTestRunner builds a runner over a migrated temp DB with one repo and
// one issue, plus a queued agent run that serves it. sessionErr, when non-nil,
// makes the injected session factory fail (simulating a missing image / docker
// down).
func newAgentTestRunner(t *testing.T, sessionErr error) (*ciRunner, storage.CIRun, *atomic.Int32, *atomic.Int32) {
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
	issue, err := storage.CreateIssue(db, repoID, api.CreateIssueRequest{
		Title: "do the thing", Body: "make it so", Author: "alice",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	n := issue.Number
	run, err := storage.EnqueueRun(db, repoID, storage.NewRun{
		Kind: storage.RunKindAgent, IssueNumber: &n,
		CommitSHA: "deadbeefcafe", Ref: "HEAD", Event: "agent",
	})
	if err != nil {
		t.Fatalf("EnqueueRun: %v", err)
	}

	var opened, closed atomic.Int32
	r := &ciRunner{
		db: db,
		cfg: &config.Config{
			DataDir:           dir,
			ReposDir:          filepath.Join(dir, "repos"),
			AgentRunTimeout:   time.Minute,
			AgentDefaultImage: "moongit-agent:latest",
		},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		newSession: func(_ context.Context, name, _, image string) (jobSession, error) {
			if image != "moongit-agent:latest" {
				t.Errorf("agent session image = %q, want moongit-agent:latest", image)
			}
			if sessionErr != nil {
				return nil, sessionErr
			}
			opened.Add(1)
			return &fakeAgentSession{closed: &closed}, nil
		},
		checkout: func(context.Context, string, string, string) error { return nil },
		readPipeline: func(string, string) ([]byte, bool, error) {
			t.Error("agent run must not read a pipeline")
			return nil, false, nil
		},
	}
	return r, run, &opened, &closed
}

// A spawned agent run checks out, opens its container, reaches the executor
// seam, and finalizes as a single successful "agent" job — the #74 done-when.
func TestExecuteAgentRunReachesSeam(t *testing.T) {
	r, run, opened, closed := newAgentTestRunner(t, nil)

	r.executeAgentRun(context.Background(), run)

	got, err := storage.GetRun(r.db, run.RepoID, run.Number)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Status != storage.RunSuccess {
		t.Errorf("run status = %q, want success", got.Status)
	}
	if opened.Load() != 1 {
		t.Errorf("sessions opened = %d, want 1", opened.Load())
	}
	if closed.Load() != 1 {
		t.Errorf("sessions closed = %d, want 1", closed.Load())
	}

	jobs, err := storage.ListJobs(r.db, run.ID)
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].Name != agentJobName {
		t.Fatalf("jobs = %+v, want one %q job", jobs, agentJobName)
	}
	if jobs[0].Status != storage.JobSuccess {
		t.Errorf("agent job status = %q, want success", jobs[0].Status)
	}

	// The transcript brackets the seam with run.started / run.completed.
	path := ci.EventLogPath(r.cfg.DataDir, "alice", "repo", run.Number, agentJobName)
	events, _, err := ci.ReadEvents(path, 0)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if !hasEventType(events, ci.EventRunStarted) || !hasEventType(events, ci.EventRunCompleted) {
		t.Errorf("event types = %v, want run.started + run.completed", eventTypes(events))
	}
}

// A session-open failure (missing image, docker down) fails the run loudly
// rather than silently — the run is errored, not left running.
func TestExecuteAgentRunSessionFailure(t *testing.T) {
	wantErr := errOpenSession
	r, run, opened, _ := newAgentTestRunner(t, wantErr)

	r.executeAgentRun(context.Background(), run)

	if opened.Load() != 0 {
		t.Errorf("sessions opened = %d, want 0 (open failed)", opened.Load())
	}
	got, err := storage.GetRun(r.db, run.RepoID, run.Number)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Status != storage.RunFailed {
		t.Errorf("run status = %q, want failed", got.Status)
	}
	jobs, _ := storage.ListJobs(r.db, run.ID)
	if len(jobs) != 1 || jobs[0].Status != storage.JobError {
		t.Errorf("jobs = %+v, want one errored job", jobs)
	}
}

var errOpenSession = errTest("docker: no such image")

type errTest string

func (e errTest) Error() string { return string(e) }

func hasEventType(events []ci.Event, typ string) bool {
	for _, ev := range events {
		if ev.Type == typ {
			return true
		}
	}
	return false
}

func eventTypes(events []ci.Event) []string {
	out := make([]string, len(events))
	for i, ev := range events {
		out[i] = ev.Type
	}
	return out
}
