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
	lines    []string // NDJSON lines claude would print on stdout
	stderr   []string // lines the tool writes to stderr
	exitCode int
	gotArgv  *[]string
	block    bool // when set, ExecStream blocks until ctx is cancelled (force-stop tests)
}

func (f *fakeAgentSession) Exec(context.Context, string) (stepResult, error) {
	return stepResult{}, nil
}

func (f *fakeAgentSession) ExecStream(ctx context.Context, argv []string, onLine, onStderr func([]byte)) (int, error) {
	if f.gotArgv != nil {
		*f.gotArgv = argv
	}
	if f.block {
		<-ctx.Done() // an operator force-stop cancels the turn ctx; mimic an aborted exec
		return -1, ctx.Err()
	}
	for _, ln := range f.lines {
		onLine([]byte(ln + "\n"))
	}
	// streamCommand replays stderr after stdout is drained; mirror that order.
	for _, ln := range f.stderr {
		if onStderr != nil {
			onStderr([]byte(ln + "\n"))
		}
	}
	return f.exitCode, nil
}

func (f *fakeAgentSession) Close() error { return nil }

// agentTestOpts configures the fake agent session newAgentTestRunner injects.
type agentTestOpts struct {
	sessionErr       error    // make the (turn-1) session factory fail
	lines            []string // claude stream-json lines the session replays
	followupLines    []string // when set, follow-up turns (attachSession) replay these instead of lines
	stderr           []string // stderr lines the session replays
	exitCode         int      // claude's process exit code
	blockUntilCancel bool     // turn-1 ExecStream blocks until ctx cancel (force-stop tests)
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
	opened    *atomic.Int32 // newAgentSession (turn-1 container open)
	attached  *atomic.Int32 // attachSession (follow-up resume)
	teardowns *atomic.Int32 // teardownContainer
	gotArgv   *[]string     // argv of the last ExecStream
	gotEnv    *[]string     // env passed to the turn-1 container
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
	var gotArgv, gotEnv []string
	fake := func() (jobSession, error) {
		return &fakeAgentSession{lines: opts.lines, stderr: opts.stderr, exitCode: opts.exitCode, gotArgv: &gotArgv, block: opts.blockUntilCancel}, nil
	}
	r := &ciRunner{
		db: db,
		cfg: &config.Config{
			Addr:                  ":8080",
			DataDir:               dir,
			ReposDir:              filepath.Join(dir, "repos"),
			AgentRunTimeout:       time.Minute,
			AgentTurnTimeout:      time.Minute,
			AgentDefaultImage:     "moongit-agent:latest",
			AgentClaudeOAuthToken: "oauth-tok",
			DexURL:                "http://dex.local",
			DexToken:              "dex-tok",
			DexProject:            "proj-1",
		},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		newAgentSession: func(_ context.Context, _, _, image string, env []string) (jobSession, error) {
			if image != "moongit-agent:latest" {
				t.Errorf("agent image = %q, want moongit-agent:latest", image)
			}
			if opts.sessionErr != nil {
				return nil, opts.sessionErr
			}
			opened.Add(1)
			gotEnv = env
			return fake()
		},
		attachSession: func(_ context.Context, _ string) (jobSession, error) {
			attached.Add(1)
			if opts.followupLines != nil {
				return &fakeAgentSession{lines: opts.followupLines, stderr: opts.stderr, exitCode: opts.exitCode, gotArgv: &gotArgv}, nil
			}
			return fake()
		},
		teardownContainer: func(string) { teardowns.Add(1) },
		checkout:          func(context.Context, string, string, string) error { return nil },
		readPipeline: func(string, string) ([]byte, bool, error) {
			t.Error("agent run must not read a pipeline")
			return nil, false, nil
		},
	}
	return agentHarness{r: r, run: run, opened: &opened, attached: &attached, teardowns: &teardowns, gotArgv: &gotArgv, gotEnv: &gotEnv}
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

// The run injects scoped per-run credentials into the container env, mints a
// real ephemeral moongit token (revoked on teardown), and wires the dex MCP.
func TestAgentRunInjectsCredentials(t *testing.T) {
	h := newAgentHarness(t, agentTestOpts{lines: successTurn})
	h.r.executeAgentRun(context.Background(), h.run)

	env := *h.gotEnv
	for k, want := range map[string]string{
		"CLAUDE_CODE_OAUTH_TOKEN": "oauth-tok",
		"MOONGIT_SERVER":          "http://host.docker.internal:8080",
		"DEX_REMOTE_URL":          "http://dex.local",
		"DEX_SERVE_TOKEN":         "dex-tok",
		"DEX_PROJECT":             "proj-1",
	} {
		if got := envValue(env, k); got != want {
			t.Errorf("env %s = %q, want %q (all: %v)", k, got, want, env)
		}
	}

	mgitTok := envValue(env, "MOONGIT_TOKEN")
	if mgitTok == "" {
		t.Fatal("MOONGIT_TOKEN not injected")
	}
	// The injected token is a real, active moongit token while parked.
	tok, err := storage.LookupToken(h.r.db, mgitTok)
	if err != nil {
		t.Fatalf("injected token not valid: %v", err)
	}
	if tok.Name != agentTokenName(h.run.ID) {
		t.Errorf("token name = %q, want %q", tok.Name, agentTokenName(h.run.ID))
	}

	// The agent MCP config (mgit + dex) is wired into the launch.
	if !argvHas(*h.gotArgv, "--mcp-config", "/work/"+agentMCPConfigName) || !argvContains(*h.gotArgv, "--strict-mcp-config") {
		t.Errorf("argv missing agent MCP config: %v", *h.gotArgv)
	}

	// On teardown the ephemeral token is revoked.
	h.r.cfg.AgentRunTimeout = time.Nanosecond
	h.r.reapExpiredAgents(context.Background())
	if _, err := storage.LookupToken(h.r.db, mgitTok); err == nil {
		t.Error("token still valid after teardown; want revoked")
	}
}

func envValue(env []string, key string) string {
	for _, e := range env {
		if strings.HasPrefix(e, key+"=") {
			return e[len(key)+1:]
		}
	}
	return ""
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

// CancelAgentRun force-stops a parked run: marks it canceled and tears down the
// container it was holding (no turn in flight) (#146).
func TestCancelAgentRunParked(t *testing.T) {
	h := newAgentHarness(t, agentTestOpts{lines: successTurn})
	h.r.executeAgentRun(context.Background(), h.run) // park (container left up)
	if got := h.status(t); got != storage.RunAwaitingInput {
		t.Fatalf("precondition: status = %q, want awaiting_input", got)
	}

	if !h.r.CancelAgentRun(h.run.ID) {
		t.Fatal("CancelAgentRun returned false for a parked run")
	}
	if got := h.status(t); got != storage.RunCanceled {
		t.Errorf("status = %q, want canceled", got)
	}
	if h.teardowns.Load() != 1 {
		t.Errorf("teardowns = %d, want 1 (cancel tears down the parked container)", h.teardowns.Load())
	}
	// A second cancel is a no-op: already terminal.
	if h.r.CancelAgentRun(h.run.ID) {
		t.Error("CancelAgentRun returned true for an already-canceled run")
	}
}

// CancelAgentRun interrupts an in-flight turn: it unblocks the turn's ExecStream
// and the turn goroutine, seeing the canceled flag, skips its own finalize — so
// the run ends RunCanceled (not RunError from the aborted exec) (#146).
func TestCancelAgentRunInterruptsInFlightTurn(t *testing.T) {
	h := newAgentHarness(t, agentTestOpts{blockUntilCancel: true})
	done := make(chan struct{})
	go func() {
		h.r.executeAgentRun(context.Background(), h.run)
		close(done)
	}()

	// Wait until the turn registers its cancel handle (it's now blocked in exec).
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, ok := h.r.agentTurns.Load(h.run.ID); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("turn never registered an in-flight handle")
		}
		time.Sleep(2 * time.Millisecond)
	}

	if !h.r.CancelAgentRun(h.run.ID) {
		t.Fatal("CancelAgentRun returned false for a running run")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("executeAgentRun did not return after cancel (turn not interrupted)")
	}

	if got := h.status(t); got != storage.RunCanceled {
		t.Errorf("status = %q, want canceled (not error from the aborted exec)", got)
	}
}

// A turn that ran but reported an error result (not an executor failure) is
// NOT terminal: the run parks awaiting_input with its container left alive, so
// the operator can Continue and course-correct. Only operator Stop or infra
// death (execErr) ends a session (#178, reversing the terminal-fail of #145).
func TestExecuteAgentRunErrorResultParks(t *testing.T) {
	h := newAgentHarness(t, agentTestOpts{
		lines: []string{`{"type":"result","subtype":"error_max_turns","is_error":true,"num_turns":5}`},
	})
	h.r.executeAgentRun(context.Background(), h.run)
	if got := h.status(t); got != storage.RunAwaitingInput {
		t.Errorf("run status = %q, want awaiting_input (a failed turn parks, not terminal)", got)
	}
	if h.teardowns.Load() != 0 {
		t.Errorf("teardowns = %d, want 0 (a parked failed turn keeps its container)", h.teardowns.Load())
	}
}

// A follow-up turn that reports an error result re-parks awaiting_input (turn
// marked done), rather than finalizing the run — the session stays resumable
// (#178).
func TestDispatchTurnErrorResultReparks(t *testing.T) {
	h := newAgentHarness(t, agentTestOpts{
		lines:         successTurn, // turn 1 parks
		followupLines: []string{`{"type":"result","subtype":"error_max_turns","is_error":true,"num_turns":5}`},
	})
	h.r.executeAgentRun(context.Background(), h.run) // park after turn 1
	if got := h.status(t); got != storage.RunAwaitingInput {
		t.Fatalf("precondition: run status = %q, want awaiting_input", got)
	}

	if _, err := storage.EnqueueTurn(h.r.db, h.run.ID, "alice", "now do the next bit"); err != nil {
		t.Fatalf("EnqueueTurn: %v", err)
	}
	turn, run, err := storage.ClaimNextTurn(h.r.db, time.Hour)
	if err != nil {
		t.Fatalf("ClaimNextTurn: %v", err)
	}
	h.r.dispatchTurn(context.Background(), turn, run)

	if got := h.status(t); got != storage.RunAwaitingInput {
		t.Errorf("run status after failed follow-up = %q, want awaiting_input (re-parked)", got)
	}
	if h.teardowns.Load() != 0 {
		t.Errorf("teardowns = %d, want 0 (a re-parked failed follow-up keeps its container)", h.teardowns.Load())
	}
	turns, _ := storage.ListTurns(h.r.db, h.run.ID)
	if len(turns) != 1 || turns[0].Status != storage.TurnDone {
		t.Errorf("turns = %+v, want one done turn", turns)
	}
}

// A failed turn whose tool wrote its diagnostics only to stderr (mooncake's
// planner errors, a crash trace) must not render blank: the stderr is replayed
// as agent.raw so the operator can see why it failed (#117).
func TestAgentTurnSurfacesStderrOnFailure(t *testing.T) {
	h := newAgentHarness(t, agentTestOpts{
		// No stdout result line + a non-zero exit => the turn is an error.
		exitCode: 1,
		stderr: []string{
			"planner setup failed: failed to build plan",
			"  line 3: cannot unmarshal !!str into config.AssertFile",
		},
	})
	h.r.executeAgentRun(context.Background(), h.run)

	var raw []string
	for _, ev := range h.events(t) {
		if ev.Type == ci.EventAgentRaw {
			if line, _ := ev.Data["line"].(string); line != "" {
				raw = append(raw, line)
			}
		}
	}
	want := []string{
		"planner setup failed: failed to build plan",
		"  line 3: cannot unmarshal !!str into config.AssertFile",
	}
	if len(raw) != len(want) {
		t.Fatalf("agent.raw lines = %v, want %v; all events: %v", raw, want, eventTypes(h.events(t)))
	}
	for i, w := range want {
		if raw[i] != w {
			t.Errorf("agent.raw[%d] = %q, want %q", i, raw[i], w)
		}
	}
}

// A successful turn keeps the transcript clean — stderr is not replayed.
func TestAgentTurnHidesStderrOnSuccess(t *testing.T) {
	h := newAgentHarness(t, agentTestOpts{
		lines:  successTurn,
		stderr: []string{"some noisy warning on stderr"},
	})
	h.r.executeAgentRun(context.Background(), h.run)
	if hasEventType(h.events(t), ci.EventAgentRaw) {
		t.Errorf("a successful turn must not surface stderr as agent.raw; got %v", eventTypes(h.events(t)))
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
