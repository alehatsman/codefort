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
// argv of the last call so launch-context composition can be asserted.
type fakeAgentSession struct {
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

func (f *fakeAgentSession) Close() error { return nil }

// agentTestOpts configures the fake agent session newAgentTestRunner injects.
type agentTestOpts struct {
	sessionErr error    // make the (turn-1) session factory fail
	lines      []string // claude stream-json lines the session replays
	exitCode   int      // claude's process exit code
}

// successTurn is a minimal, well-formed claude stream-json turn that ends ok.
var successTurn = []string{
	`{"type":"system","subtype":"init","session_id":"s","model":"claude"}`,
	`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}`,
	`{"type":"result","subtype":"success","is_error":false,"num_turns":1,"duration_ms":12,"total_cost_usd":0.001}`,
}

// agentHarness bundles a runner over a migrated temp DB (one repo, one issue,
// one queued agent run) plus the counters the injected fakes record.
type agentHarness struct {
	r         *ciRunner
	run       storage.CIRun
	opened    *atomic.Int32 // newSession (turn-1 container open)
	attached  *atomic.Int32 // attachSession (follow-up resume)
	teardowns *atomic.Int32 // teardownContainer
	gotArgv   *[]string     // argv of the last ExecStream
}

func newAgentHarness(t *testing.T, opts agentTestOpts) agentHarness {
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
	if _, err := storage.EnqueueRun(db, repoID, storage.NewRun{
		Kind: storage.RunKindAgent, IssueNumber: &n,
		CommitSHA: "deadbeefcafe", Ref: "HEAD", Event: "agent",
	}); err != nil {
		t.Fatalf("EnqueueRun: %v", err)
	}
	// Claim it (queued -> running) exactly as drainKind does before dispatch,
	// so executeAgentRun sees a running run it can park.
	run, err := storage.ClaimNextRunOfKind(db, storage.RunKindAgent, time.Hour)
	if err != nil {
		t.Fatalf("ClaimNextRunOfKind: %v", err)
	}

	var opened, attached, teardowns atomic.Int32
	var gotArgv []string
	fake := func() (jobSession, error) {
		return &fakeAgentSession{lines: opts.lines, exitCode: opts.exitCode, gotArgv: &gotArgv}, nil
	}
	r := &ciRunner{
		db: db,
		cfg: &config.Config{
			DataDir:           dir,
			ReposDir:          filepath.Join(dir, "repos"),
			AgentRunTimeout:   time.Minute,
			AgentTurnTimeout:  time.Minute,
			AgentDefaultImage: "moongit-agent:latest",
		},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		newSession: func(_ context.Context, _, _, image string) (jobSession, error) {
			if image != "moongit-agent:latest" {
				t.Errorf("agent image = %q, want moongit-agent:latest", image)
			}
			if opts.sessionErr != nil {
				return nil, opts.sessionErr
			}
			opened.Add(1)
			return fake()
		},
		attachSession: func(_ context.Context, _ string) (jobSession, error) {
			attached.Add(1)
			return fake()
		},
		teardownContainer: func(string) { teardowns.Add(1) },
		checkout:          func(context.Context, string, string, string) error { return nil },
		readPipeline: func(string, string) ([]byte, bool, error) {
			t.Error("agent run must not read a pipeline")
			return nil, false, nil
		},
	}
	return agentHarness{r: r, run: run, opened: &opened, attached: &attached, teardowns: &teardowns, gotArgv: &gotArgv}
}

func (h agentHarness) status(t *testing.T) storage.RunStatus {
	t.Helper()
	got, err := storage.GetRun(h.r.db, h.run.RepoID, h.run.Number)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	return got.Status
}

func (h agentHarness) events(t *testing.T) []ci.Event {
	t.Helper()
	path := ci.EventLogPath(h.r.cfg.DataDir, "alice", "repo", h.run.Number, agentJobName)
	evs, _, err := ci.ReadEvents(path, 0)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	return evs
}

// Turn 1 streams, then the run parks in awaiting_input with its container left
// running (not torn down) so a follow-up turn can resume into it.
func TestExecuteAgentRunParksAfterTurn1(t *testing.T) {
	h := newAgentHarness(t, agentTestOpts{lines: successTurn})

	h.r.executeAgentRun(context.Background(), h.run)

	if got := h.status(t); got != storage.RunAwaitingInput {
		t.Errorf("run status = %q, want awaiting_input", got)
	}
	if h.opened.Load() != 1 {
		t.Errorf("containers opened = %d, want 1", h.opened.Load())
	}
	if h.teardowns.Load() != 0 {
		t.Errorf("teardowns = %d, want 0 (container kept alive for next turn)", h.teardowns.Load())
	}
	// The agent job stays running across the parked session.
	jobs, _ := storage.ListJobs(h.r.db, h.run.ID)
	if len(jobs) != 1 || jobs[0].Status != storage.JobRunning {
		t.Errorf("jobs = %+v, want one running agent job", jobs)
	}
	for _, want := range []string{ci.EventRunStarted, ci.EventAgentTurnStarted, ci.EventAgentMessage, ci.EventAgentTurnCompleted} {
		if !hasEventType(h.events(t), want) {
			t.Errorf("missing event %q; got %v", want, eventTypes(h.events(t)))
		}
	}
	// Turn 1 uses --session-id, not --resume, and carries the issue body.
	argv := *h.gotArgv
	if !argvHas(argv, "--session-id", agentSessionID(h.run.ID)) || argvContains(argv, "--resume") {
		t.Errorf("turn 1 argv wrong: %v", argv)
	}
	if !argvContains(argv, "make it so") {
		t.Errorf("turn 1 prompt should carry the issue body: %v", argv)
	}
}

// A follow-up turn resumes the session in the running container and re-parks.
func TestDispatchTurnResumesAndReparks(t *testing.T) {
	h := newAgentHarness(t, agentTestOpts{lines: successTurn})
	h.r.executeAgentRun(context.Background(), h.run) // park after turn 1

	if _, err := storage.EnqueueTurn(h.r.db, h.run.ID, "alice", "now do the next bit"); err != nil {
		t.Fatalf("EnqueueTurn: %v", err)
	}
	turn, run, err := storage.ClaimNextTurn(h.r.db, time.Hour)
	if err != nil {
		t.Fatalf("ClaimNextTurn: %v", err)
	}

	h.r.dispatchTurn(context.Background(), turn, run)

	if h.attached.Load() != 1 {
		t.Errorf("attach count = %d, want 1 (resumed into the running container)", h.attached.Load())
	}
	if h.opened.Load() != 1 {
		t.Errorf("opened = %d, want 1 (no second container)", h.opened.Load())
	}
	if got := h.status(t); got != storage.RunAwaitingInput {
		t.Errorf("run status after follow-up = %q, want awaiting_input", got)
	}
	turns, _ := storage.ListTurns(h.r.db, h.run.ID)
	if len(turns) != 1 || turns[0].Status != storage.TurnDone {
		t.Errorf("turns = %+v, want one done turn", turns)
	}
	// The follow-up resumes the session, carrying the user message.
	argv := *h.gotArgv
	if !argvHas(argv, "--resume", agentSessionID(h.run.ID)) || argvContains(argv, "--session-id") {
		t.Errorf("follow-up must --resume: %v", argv)
	}
	if !argvContains(argv, "now do the next bit") {
		t.Errorf("follow-up prompt should carry the message: %v", argv)
	}
}

// The lifetime reaper finalizes a parked run and tears down its container.
func TestReapExpiredAgents(t *testing.T) {
	h := newAgentHarness(t, agentTestOpts{lines: successTurn})
	h.r.executeAgentRun(context.Background(), h.run) // park
	if got := h.status(t); got != storage.RunAwaitingInput {
		t.Fatalf("precondition: run status = %q, want awaiting_input", got)
	}

	// Shrink the lifetime cap so the parked run is immediately expired.
	h.r.cfg.AgentRunTimeout = time.Nanosecond
	h.r.reapExpiredAgents(context.Background())

	if got := h.status(t); got != storage.RunCanceled {
		t.Errorf("run status after reap = %q, want canceled", got)
	}
	if h.teardowns.Load() != 1 {
		t.Errorf("teardowns = %d, want 1", h.teardowns.Load())
	}
}

// claude reporting an error result (not an executor failure) still parks — the
// human can course-correct in the next turn.
func TestExecuteAgentRunErrorResultParks(t *testing.T) {
	h := newAgentHarness(t, agentTestOpts{
		lines: []string{`{"type":"result","subtype":"error_max_turns","is_error":true,"num_turns":5}`},
	})
	h.r.executeAgentRun(context.Background(), h.run)
	if got := h.status(t); got != storage.RunAwaitingInput {
		t.Errorf("run status = %q, want awaiting_input (recoverable)", got)
	}
}

// A container that won't open fails the run loudly and tears down.
func TestExecuteAgentRunSessionFailure(t *testing.T) {
	h := newAgentHarness(t, agentTestOpts{sessionErr: errOpenSession})
	h.r.executeAgentRun(context.Background(), h.run)

	if h.opened.Load() != 0 {
		t.Errorf("opened = %d, want 0 (open failed)", h.opened.Load())
	}
	if got := h.status(t); got != storage.RunError {
		t.Errorf("run status = %q, want error", got)
	}
	if h.teardowns.Load() != 1 {
		t.Errorf("teardowns = %d, want 1 (cleanup on failure)", h.teardowns.Load())
	}
	jobs, _ := storage.ListJobs(h.r.db, h.run.ID)
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
