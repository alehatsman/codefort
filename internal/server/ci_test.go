package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alehatsman/moongit/internal/api"
	"github.com/alehatsman/moongit/internal/ci"
	"github.com/alehatsman/moongit/internal/storage"
)

// newCIReadServer extends the CI hook test server with a read pool (handlers
// read via rdb) and an on-disk DataDir for event logs.
func newCIReadServer(t *testing.T) (*Server, int64) {
	t.Helper()
	s, repoID := newCITestServer(t, "sekret")
	s.rdb = s.db
	s.cfg.DataDir = t.TempDir()
	return s, repoID
}

// ciReq drives a request through the API mux (so {owner}/{repo}/... path
// values are populated) with an authenticated identity on the context.
func ciReq(t *testing.T, s *Server, method, path, lastEventID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	ctx := context.WithValue(req.Context(), tokenCtxKey{}, api.Token{Name: "agent#17"})
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	s.apiHandler().ServeHTTP(rr, req)
	return rr
}

func enqueue(t *testing.T, s *Server, repoID int64, sha, ref string) storage.CIRun {
	t.Helper()
	run, err := storage.EnqueueRun(s.db, repoID, storage.NewRun{CommitSHA: sha, Ref: ref, Event: "push", Trigger: "alice"})
	if err != nil {
		t.Fatalf("EnqueueRun: %v", err)
	}
	return run
}

func TestListCIRunsEmpty(t *testing.T) {
	s, _ := newCIReadServer(t)
	rr := ciReq(t, s, http.MethodGet, "/api/repos/alice/repo/ci/runs", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if got := strings.TrimSpace(rr.Body.String()); got != "[]" {
		t.Errorf("body = %q, want []", got)
	}
}

func TestListCIRunsNewestFirst(t *testing.T) {
	s, repoID := newCIReadServer(t)
	enqueue(t, s, repoID, "aaa", "refs/heads/main")
	enqueue(t, s, repoID, "bbb", "refs/heads/main")
	enqueue(t, s, repoID, "ccc", "refs/heads/feat")

	rr := ciReq(t, s, http.MethodGet, "/api/repos/alice/repo/ci/runs", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var runs []api.CIRun
	if err := json.Unmarshal(rr.Body.Bytes(), &runs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(runs) != 3 {
		t.Fatalf("runs = %d, want 3", len(runs))
	}
	if runs[0].Number != 3 || runs[1].Number != 2 || runs[2].Number != 1 {
		t.Errorf("order = %d,%d,%d, want 3,2,1", runs[0].Number, runs[1].Number, runs[2].Number)
	}
	if runs[0].CommitSHA != "ccc" || runs[0].Status != "queued" {
		t.Errorf("newest run = %+v", runs[0])
	}
}

func TestListCIRunsLimit(t *testing.T) {
	s, repoID := newCIReadServer(t)
	for range 5 {
		enqueue(t, s, repoID, "sha", "refs/heads/main")
	}
	rr := ciReq(t, s, http.MethodGet, "/api/repos/alice/repo/ci/runs?limit=2", "")
	var runs []api.CIRun
	json.Unmarshal(rr.Body.Bytes(), &runs)
	if len(runs) != 2 {
		t.Fatalf("runs = %d, want 2 (limit)", len(runs))
	}

	rr = ciReq(t, s, http.MethodGet, "/api/repos/alice/repo/ci/runs?limit=-1", "")
	if rr.Code != http.StatusBadRequest {
		t.Errorf("negative limit: code = %d, want 400", rr.Code)
	}
}

func TestGetCIRunDetail(t *testing.T) {
	s, repoID := newCIReadServer(t)
	run := enqueue(t, s, repoID, "deadbeef", "refs/heads/main")
	build, err := storage.CreateJob(s.db, run.ID, "build", nil)
	if err != nil {
		t.Fatal(err)
	}
	zero := 0
	if err := storage.FinishJob(s.db, build.ID, storage.JobSuccess, &zero); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.CreateJob(s.db, run.ID, "lint", nil); err != nil {
		t.Fatal(err)
	}

	rr := ciReq(t, s, http.MethodGet, "/api/repos/alice/repo/ci/runs/1", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	var detail api.CIRunDetail
	if err := json.Unmarshal(rr.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if detail.Number != 1 || detail.CommitSHA != "deadbeef" {
		t.Errorf("run = %+v", detail.CIRun)
	}
	if len(detail.Jobs) != 2 {
		t.Fatalf("jobs = %d, want 2", len(detail.Jobs))
	}
	if detail.Jobs[0].Name != "build" || detail.Jobs[0].Status != "success" {
		t.Errorf("job[0] = %+v", detail.Jobs[0])
	}
	if detail.Jobs[0].ExitCode == nil || *detail.Jobs[0].ExitCode != 0 {
		t.Errorf("job[0] exit = %v, want 0", detail.Jobs[0].ExitCode)
	}
}

func TestGetCIRunNotFound(t *testing.T) {
	s, _ := newCIReadServer(t)
	rr := ciReq(t, s, http.MethodGet, "/api/repos/alice/repo/ci/runs/99", "")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", rr.Code)
	}
}

func TestGetCIRunBadNumber(t *testing.T) {
	s, _ := newCIReadServer(t)
	rr := ciReq(t, s, http.MethodGet, "/api/repos/alice/repo/ci/runs/abc", "")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", rr.Code)
	}
}

func TestRerunEnqueuesNewRun(t *testing.T) {
	s, repoID := newCIReadServer(t)
	storage.SetRepoCIEnabled(s.db, repoID, true)
	enqueue(t, s, repoID, "cafef00d", "refs/heads/main")

	rr := ciReq(t, s, http.MethodPost, "/api/repos/alice/repo/ci/runs/1/rerun", "")
	if rr.Code != http.StatusAccepted {
		t.Fatalf("code = %d, want 202; body=%s", rr.Code, rr.Body.String())
	}
	var run api.CIRun
	if err := json.Unmarshal(rr.Body.Bytes(), &run); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if run.Number != 2 {
		t.Errorf("rerun number = %d, want 2 (fresh)", run.Number)
	}
	if run.CommitSHA != "cafef00d" || run.Ref != "refs/heads/main" {
		t.Errorf("rerun did not copy commit/ref: %+v", run)
	}
	if run.Trigger != "agent#17" {
		t.Errorf("trigger = %q, want the requesting identity", run.Trigger)
	}
	if run.Status != "queued" {
		t.Errorf("status = %q, want queued", run.Status)
	}
	if n := runCount(t, s, repoID); n != 2 {
		t.Errorf("total runs = %d, want 2", n)
	}
}

func TestRerunDisabledRepo(t *testing.T) {
	s, repoID := newCIReadServer(t) // CI disabled
	enqueue(t, s, repoID, "sha", "refs/heads/main")
	rr := ciReq(t, s, http.MethodPost, "/api/repos/alice/repo/ci/runs/1/rerun", "")
	if rr.Code != http.StatusConflict {
		t.Fatalf("code = %d, want 409 (CI disabled)", rr.Code)
	}
}

func TestRerunUnknownRun(t *testing.T) {
	s, repoID := newCIReadServer(t)
	storage.SetRepoCIEnabled(s.db, repoID, true)
	rr := ciReq(t, s, http.MethodPost, "/api/repos/alice/repo/ci/runs/42/rerun", "")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", rr.Code)
	}
}

// writeJobEvents appends a few events to a job's on-disk stream, then makes the
// run terminal so the SSE handler replays and closes instead of tailing.
func writeJobEvents(t *testing.T, s *Server, run storage.CIRun, job string, types ...string) {
	t.Helper()
	if _, err := storage.CreateJob(s.db, run.ID, job, nil); err != nil {
		t.Fatal(err)
	}
	elog, err := ci.OpenEventLog(s.cfg.DataDir, "alice", "repo", run.Number, job)
	if err != nil {
		t.Fatalf("OpenEventLog: %v", err)
	}
	for _, ty := range types {
		if _, err := elog.Append(ty, map[string]any{"k": "v"}); err != nil {
			t.Fatal(err)
		}
	}
	elog.Close()
	if err := storage.FinishRun(s.db, run.ID, storage.RunSuccess); err != nil {
		t.Fatal(err)
	}
}

func TestJobEventsReplay(t *testing.T) {
	s, repoID := newCIReadServer(t)
	run := enqueue(t, s, repoID, "sha", "refs/heads/main")
	writeJobEvents(t, s, run, "build", ci.EventRunStarted, ci.EventStepStarted, ci.EventRunCompleted)

	rr := ciReq(t, s, http.MethodGet, "/api/repos/alice/repo/ci/runs/1/jobs/build/events", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("content-type = %q, want text/event-stream", ct)
	}
	body := rr.Body.String()
	for _, want := range []string{"id: 1", "id: 2", "id: 3", "event: " + ci.EventRunStarted, "event: " + ci.EventRunCompleted} {
		if !strings.Contains(body, want) {
			t.Errorf("stream missing %q\n--- body ---\n%s", want, body)
		}
	}
}

func TestJobEventsResume(t *testing.T) {
	s, repoID := newCIReadServer(t)
	run := enqueue(t, s, repoID, "sha", "refs/heads/main")
	writeJobEvents(t, s, run, "build", ci.EventRunStarted, ci.EventStepStarted, ci.EventRunCompleted)

	// Resume after seq 2: only seq 3 should be sent.
	rr := ciReq(t, s, http.MethodGet, "/api/repos/alice/repo/ci/runs/1/jobs/build/events", "2")
	body := rr.Body.String()
	if strings.Contains(body, "id: 1") || strings.Contains(body, "id: 2") {
		t.Errorf("resume re-sent already-seen events:\n%s", body)
	}
	if !strings.Contains(body, "id: 3") {
		t.Errorf("resume dropped the new event:\n%s", body)
	}
}

func TestJobEventsUnknownJob(t *testing.T) {
	s, repoID := newCIReadServer(t)
	run := enqueue(t, s, repoID, "sha", "refs/heads/main")
	storage.FinishRun(s.db, run.ID, storage.RunSuccess)
	rr := ciReq(t, s, http.MethodGet, "/api/repos/alice/repo/ci/runs/1/jobs/ghost/events", "")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", rr.Code)
	}
}

// A skipped job has a DB row but never writes an event file. The stream must
// still close (driven by the terminal run status) rather than tail forever.
func TestJobEventsSkippedJobClosesEmpty(t *testing.T) {
	s, repoID := newCIReadServer(t)
	run := enqueue(t, s, repoID, "sha", "refs/heads/main")
	if _, err := storage.CreateJob(s.db, run.ID, "skipped", nil); err != nil {
		t.Fatal(err)
	}
	storage.FinishRun(s.db, run.ID, storage.RunSuccess)

	rr := ciReq(t, s, http.MethodGet, "/api/repos/alice/repo/ci/runs/1/jobs/skipped/events", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rr.Code)
	}
	if strings.Contains(rr.Body.String(), "event:") {
		t.Errorf("expected no events for a skipped job, got:\n%s", rr.Body.String())
	}
}

// An agent run parked at awaiting_input is non-terminal but won't emit again
// until the next turn. The stream must drain the turn's events and CLOSE — a
// finite response flushes through a buffering proxy/SSH tunnel, whereas an
// indefinitely-open stream delivers nothing to a viewer behind one (#140).
func TestJobEventsAwaitingInputDrainsAndCloses(t *testing.T) {
	s, repoID := newCIReadServer(t)
	n := 1
	run, err := storage.EnqueueRun(s.db, repoID, storage.NewRun{
		Kind: storage.RunKindAgent, IssueNumber: &n, CommitSHA: "a", Ref: "HEAD", Event: "agent",
	})
	if err != nil {
		t.Fatalf("EnqueueRun: %v", err)
	}
	// queued -> running (claim) is the precondition for parking.
	if _, err := storage.ClaimNextRunOfKind(s.db, storage.RunKindAgent, 0); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if _, err := storage.CreateJob(s.db, run.ID, "agent", nil); err != nil {
		t.Fatal(err)
	}
	elog, err := ci.OpenEventLog(s.cfg.DataDir, "alice", "repo", run.Number, "agent")
	if err != nil {
		t.Fatalf("OpenEventLog: %v", err)
	}
	for _, ty := range []string{ci.EventRunStarted, ci.EventRunCompleted} {
		if _, err := elog.Append(ty, map[string]any{"k": "v"}); err != nil {
			t.Fatal(err)
		}
	}
	elog.Close()
	if err := storage.MarkRunAwaitingInput(s.db, run.ID); err != nil {
		t.Fatalf("park: %v", err)
	}

	// If the handler tailed an awaiting_input run forever, the synchronous
	// recorder would never return; receiving on done proves the stream closed.
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- ciReq(t, s, http.MethodGet, "/api/repos/alice/repo/ci/runs/1/jobs/agent/events", "")
	}()
	select {
	case rr := <-done:
		if rr.Code != http.StatusOK {
			t.Fatalf("code = %d, want 200", rr.Code)
		}
		body := rr.Body.String()
		for _, want := range []string{"id: 1", "id: 2", "event: " + ci.EventRunStarted, "event: " + ci.EventRunCompleted} {
			if !strings.Contains(body, want) {
				t.Errorf("stream missing %q\n--- body ---\n%s", want, body)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not close for an awaiting_input run (hung)")
	}
}

func TestUpdateRepoCIEnabled(t *testing.T) {
	s, repoID := newCIReadServer(t)

	// GET reflects the default (disabled).
	rr := ciReq(t, s, http.MethodGet, "/api/repos/alice/repo", "")
	var repo api.Repo
	json.Unmarshal(rr.Body.Bytes(), &repo)
	if repo.CIEnabled {
		t.Fatalf("ci_enabled = true, want false by default")
	}

	// PATCH enables it.
	body := strings.NewReader(`{"ci_enabled":true}`)
	req := httptest.NewRequest(http.MethodPatch, "/api/repos/alice/repo", body)
	ctx := context.WithValue(req.Context(), tokenCtxKey{}, api.Token{Name: "agent#17"})
	rr = httptest.NewRecorder()
	s.apiHandler().ServeHTTP(rr, req.WithContext(ctx))
	if rr.Code != http.StatusOK {
		t.Fatalf("PATCH code = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	json.Unmarshal(rr.Body.Bytes(), &repo)
	if !repo.CIEnabled {
		t.Errorf("after PATCH ci_enabled = false, want true")
	}
	if enabled, _ := storage.RepoCIEnabled(s.db, repoID); !enabled {
		t.Errorf("storage ci_enabled not persisted")
	}
}

func TestUpdateRepoNoFields(t *testing.T) {
	s, _ := newCIReadServer(t)
	req := httptest.NewRequest(http.MethodPatch, "/api/repos/alice/repo", strings.NewReader(`{}`))
	rr := httptest.NewRecorder()
	s.apiHandler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400 (no fields)", rr.Code)
	}
}

func TestJobEventsRunNotFound(t *testing.T) {
	s, _ := newCIReadServer(t)
	rr := ciReq(t, s, http.MethodGet, "/api/repos/alice/repo/ci/runs/7/jobs/build/events", "")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", rr.Code)
	}
}

// TestJobEventsThroughFullHandler exercises the SSE endpoint through the
// complete Handler() chain — crucially including withLogging, whose
// statusRecorder wraps the ResponseWriter. The other JobEvents tests drive
// apiHandler() directly, where the bare httptest.ResponseRecorder already
// satisfies http.Flusher, so they never caught statusRecorder dropping that
// interface (issue #40: every request returned 500 "streaming unsupported").
func TestJobEventsThroughFullHandler(t *testing.T) {
	s, repoID := newCIReadServer(t)
	run := enqueue(t, s, repoID, "sha", "refs/heads/main")
	writeJobEvents(t, s, run, "build", ci.EventRunStarted, ci.EventRunCompleted)

	raw, err := storage.GenerateTokenString()
	if err != nil {
		t.Fatalf("GenerateTokenString: %v", err)
	}
	if _, err := storage.CreateToken(s.db, "agent#17", raw); err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/repos/alice/repo/ci/runs/1/jobs/build/events", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("content-type = %q, want text/event-stream", ct)
	}
	if body := rr.Body.String(); !strings.Contains(body, "event: "+ci.EventRunCompleted) {
		t.Errorf("stream missing events through full chain:\n%s", body)
	}
}

// safeJobName guards the not-yet-created-job streaming path against traversal.
func TestSafeJobName(t *testing.T) {
	for _, ok := range []string{"agent", "build", "step-1", "a.b_c"} {
		if !safeJobName(ok) {
			t.Errorf("safeJobName(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", ".", "..", "a/b", `a\b`, "../etc"} {
		if safeJobName(bad) {
			t.Errorf("safeJobName(%q) = true, want false", bad)
		}
	}
}

// A not-yet-created job (e.g. an agent run's "agent" job before the runner
// creates it) on a still-live run streams rather than 404ing — the client
// connects the moment it navigates in. A terminal run still 404s an unknown
// job (TestJobEventsUnknownJob). The request is cancelled to unblock the SSE
// tail loop.
func TestJobEventsPendingJobOnLiveRunStreams(t *testing.T) {
	s, repoID := newCIReadServer(t)
	enqueue(t, s, repoID, "sha", "refs/heads/main") // queued (non-terminal), no jobs yet

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/repos/alice/repo/ci/runs/1/jobs/agent/events", nil).WithContext(ctx)
	req.SetPathValue("owner", "alice")
	req.SetPathValue("repo", "repo")
	req.SetPathValue("number", "1")
	req.SetPathValue("job", "agent")
	rr := httptest.NewRecorder()
	s.handleCIJobEvents(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200 (stream-and-wait, not 404); body=%s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "job not found") {
		t.Errorf("got a 'job not found' error for a live run's pending job:\n%s", rr.Body.String())
	}
}
