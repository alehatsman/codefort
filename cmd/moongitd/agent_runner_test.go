package main

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alehatsman/moongit/internal/api"
	"github.com/alehatsman/moongit/internal/ci"
	"github.com/alehatsman/moongit/internal/config"
	"github.com/alehatsman/moongit/internal/storage"
)

// fakeAgentSession is a jobSession + streamingSession that replays canned
// claude stream-json lines instead of execing a real binary. It records the
// argv it was handed so launch-context composition can be asserted.
type fakeAgentSession struct {
	closed   *atomic.Int32
	lines    []string // NDJSON lines claude would print
	exitCode int
	gotArgv  *[]string
}

func (f *fakeAgentSession) Exec(context.Context, string) (stepResult, error) {
	return stepResult{}, nil
}

func (f *fakeAgentSession) ExecStream(_ context.Context, argv []string, onLine func([]byte)) (int, error) {
	if f.gotArgv != nil {
		*f.gotArgv = argv
	}
	for _, ln := range f.lines {
		onLine([]byte(ln + "\n"))
	}
	return f.exitCode, nil
}

func (f *fakeAgentSession) Close() error { f.closed.Add(1); return nil }

// agentTestOpts configures the fake agent session newAgentTestRunner injects.
type agentTestOpts struct {
	sessionErr error    // make the session factory fail (missing image / docker down)
	lines      []string // claude stream-json lines the session replays
	exitCode   int      // claude's process exit code
}

// successTurn is a minimal, well-formed claude stream-json turn that ends ok.
var successTurn = []string{
	`{"type":"system","subtype":"init","session_id":"s","model":"claude"}`,
	`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}`,
	`{"type":"result","subtype":"success","is_error":false,"num_turns":1,"duration_ms":12,"total_cost_usd":0.001}`,
}

// newAgentTestRunner builds a runner over a migrated temp DB with one repo and
// one issue, plus a queued agent run that serves it. It returns the runner, the
// run, open/close counters, and a pointer that captures the argv handed to the
// session (for launch-context assertions).
func newAgentTestRunner(t *testing.T, opts agentTestOpts) (*ciRunner, storage.CIRun, *atomic.Int32, *atomic.Int32, *[]string) {
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
	var gotArgv []string
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
			if opts.sessionErr != nil {
				return nil, opts.sessionErr
			}
			opened.Add(1)
			return &fakeAgentSession{
				closed: &closed, lines: opts.lines, exitCode: opts.exitCode, gotArgv: &gotArgv,
			}, nil
		},
		checkout: func(context.Context, string, string, string) error { return nil },
		readPipeline: func(string, string) ([]byte, bool, error) {
			t.Error("agent run must not read a pipeline")
			return nil, false, nil
		},
	}
	return r, run, &opened, &closed, &gotArgv
}

// A spawned agent run checks out, opens its container, streams a claude turn
// onto the event log, and finalizes as a single successful "agent" job — the
// #76 done-when for a single turn.
func TestExecuteAgentRunStreamsTurn(t *testing.T) {
	r, run, opened, closed, gotArgv := newAgentTestRunner(t, agentTestOpts{lines: successTurn})

	r.executeAgentRun(context.Background(), run)

	got, err := storage.GetRun(r.db, run.RepoID, run.Number)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Status != storage.RunSuccess {
		t.Errorf("run status = %q, want success", got.Status)
	}
	if opened.Load() != 1 || closed.Load() != 1 {
		t.Errorf("session open/close = %d/%d, want 1/1", opened.Load(), closed.Load())
	}

	jobs, err := storage.ListJobs(r.db, run.ID)
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].Name != agentJobName || jobs[0].Status != storage.JobSuccess {
		t.Fatalf("jobs = %+v, want one successful %q job", jobs, agentJobName)
	}

	// The transcript brackets the turn and carries each claude line as an
	// agent.message, ending run.completed.
	path := ci.EventLogPath(r.cfg.DataDir, "alice", "repo", run.Number, agentJobName)
	events, _, err := ci.ReadEvents(path, 0)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	for _, want := range []string{
		ci.EventRunStarted, ci.EventAgentTurnStarted, ci.EventAgentMessage,
		ci.EventAgentTurnCompleted, ci.EventRunCompleted,
	} {
		if !hasEventType(events, want) {
			t.Errorf("missing event %q; got %v", want, eventTypes(events))
		}
	}
	// Three claude lines -> three agent.message events.
	if n := countEventType(events, ci.EventAgentMessage); n != 3 {
		t.Errorf("agent.message count = %d, want 3", n)
	}

	// Launch context: headless stream-json, bypassPermissions, the run's session
	// id (not --resume on turn 1), and the issue body as the prompt.
	argv := *gotArgv
	if !argvHas(argv, "--output-format", "stream-json") || !argvHas(argv, "--permission-mode", "bypassPermissions") {
		t.Errorf("argv missing stream-json/bypassPermissions: %v", argv)
	}
	if !argvHas(argv, "--session-id", agentSessionID(run.ID)) {
		t.Errorf("argv missing --session-id %s: %v", agentSessionID(run.ID), argv)
	}
	if argvContains(argv, "--resume") {
		t.Errorf("turn 1 should use --session-id, not --resume: %v", argv)
	}
	if !argvContains(argv, "make it so") {
		t.Errorf("argv prompt should carry the issue body: %v", argv)
	}
}

// claude running but reporting an error result fails the run (distinct from an
// executor failure): the job is failed, not errored.
func TestExecuteAgentRunErrorResult(t *testing.T) {
	r, run, _, _, _ := newAgentTestRunner(t, agentTestOpts{
		lines: []string{`{"type":"result","subtype":"error_max_turns","is_error":true,"num_turns":5}`},
	})

	r.executeAgentRun(context.Background(), run)

	got, _ := storage.GetRun(r.db, run.RepoID, run.Number)
	if got.Status != storage.RunFailed {
		t.Errorf("run status = %q, want failed", got.Status)
	}
	jobs, _ := storage.ListJobs(r.db, run.ID)
	if len(jobs) != 1 || jobs[0].Status != storage.JobFailed {
		t.Errorf("jobs = %+v, want one failed job", jobs)
	}
}

// A session-open failure (missing image, docker down) fails the run loudly
// rather than silently — the run is errored, not left running.
func TestExecuteAgentRunSessionFailure(t *testing.T) {
	r, run, opened, _, _ := newAgentTestRunner(t, agentTestOpts{sessionErr: errOpenSession})

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

func countEventType(events []ci.Event, typ string) int {
	n := 0
	for _, ev := range events {
		if ev.Type == typ {
			n++
		}
	}
	return n
}

// argvContains reports whether s appears as a substring of any argv element
// (used to find the prompt, which is one big positional arg).
func argvContains(argv []string, s string) bool {
	for _, a := range argv {
		if strings.Contains(a, s) {
			return true
		}
	}
	return false
}

// argvHas reports whether argv contains flag immediately followed by value.
func argvHas(argv []string, flag, value string) bool {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == flag && argv[i+1] == value {
			return true
		}
	}
	return false
}
