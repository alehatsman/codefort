package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/alehatsman/moongit/internal/api"
	"github.com/alehatsman/moongit/internal/storage"
)

// spawnAgent drives handleSpawnAgent directly for issue `num`, with an optional
// base ref body and an authenticated identity.
func spawnAgent(t *testing.T, s *Server, num int, ref string) *httptest.ResponseRecorder {
	t.Helper()
	var body []byte
	if ref != "" {
		body, _ = json.Marshal(api.SpawnAgentRequest{Ref: ref})
	}
	req := httptest.NewRequest(http.MethodPost, "/api/repos/alice/repo/issues/"+strconv.Itoa(num)+"/agent", bytes.NewReader(body))
	req.SetPathValue("owner", "alice")
	req.SetPathValue("repo", "repo")
	req.SetPathValue("number", strconv.Itoa(num))
	req = req.WithContext(context.WithValue(req.Context(), tokenCtxKey{}, api.Token{Name: "agent#17"}))
	rr := httptest.NewRecorder()
	s.handleSpawnAgent(rr, req)
	return rr
}

// Spawning an agent for an existing issue enqueues a kind=agent run linked to
// the issue, resolved against the repo's HEAD, triggered by the caller.
func TestSpawnAgentEnqueuesAgentRun(t *testing.T) {
	s, repoID := newCITriggerServer(t)
	issue, err := storage.CreateIssue(s.db, repoID, api.CreateIssueRequest{
		Title: "do the thing", Author: "alice",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	rr := spawnAgent(t, s, issue.Number, "")
	if rr.Code != http.StatusAccepted {
		t.Fatalf("code = %d, want 202; body=%s", rr.Code, rr.Body.String())
	}

	var run api.CIRun
	if err := json.Unmarshal(rr.Body.Bytes(), &run); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if run.Kind != "agent" {
		t.Errorf("kind = %q, want agent", run.Kind)
	}
	if run.IssueNumber == nil || *run.IssueNumber != issue.Number {
		t.Errorf("issue_number = %v, want %d", run.IssueNumber, issue.Number)
	}
	if run.Event != "agent" {
		t.Errorf("event = %q, want agent", run.Event)
	}
	if run.Trigger != "agent#17" {
		t.Errorf("trigger = %q, want agent#17", run.Trigger)
	}
	if run.CommitSHA == "" {
		t.Error("commit_sha empty; HEAD should have resolved")
	}

	// It landed as a queued agent run in storage.
	stored, err := storage.GetRun(s.db, repoID, run.Number)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if stored.Kind != storage.RunKindAgent || stored.Status != storage.RunQueued {
		t.Errorf("stored = %s/%s, want agent/queued", stored.Kind, stored.Status)
	}
}

// Spawning for an issue that doesn't exist is a 404, before anything is queued.
func TestSpawnAgentUnknownIssue(t *testing.T) {
	s, _ := newCITriggerServer(t)
	rr := spawnAgent(t, s, 999, "")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404; body=%s", rr.Code, rr.Body.String())
	}
}

// createTurn drives handleCreateAgentTurn for run `num` with a message body.
func createTurn(t *testing.T, s *Server, num int, text string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(api.CreateAgentTurnRequest{Text: text})
	req := httptest.NewRequest(http.MethodPost, "/api/repos/alice/repo/runs/"+strconv.Itoa(num)+"/turns", bytes.NewReader(body))
	req.SetPathValue("owner", "alice")
	req.SetPathValue("repo", "repo")
	req.SetPathValue("number", strconv.Itoa(num))
	req = req.WithContext(context.WithValue(req.Context(), tokenCtxKey{}, api.Token{Name: "agent#17"}))
	rr := httptest.NewRecorder()
	s.handleCreateAgentTurn(rr, req)
	return rr
}

// A follow-up turn on an agent run queues a pending turn linked to the run.
func TestCreateAgentTurnQueues(t *testing.T) {
	s, repoID := newCITriggerServer(t)
	issue, err := storage.CreateIssue(s.db, repoID, api.CreateIssueRequest{Title: "x", Author: "alice"})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	n := issue.Number
	run, err := storage.EnqueueRun(s.db, repoID, storage.NewRun{
		Kind: storage.RunKindAgent, IssueNumber: &n, CommitSHA: "abc", Ref: "HEAD", Event: "agent",
	})
	if err != nil {
		t.Fatalf("EnqueueRun: %v", err)
	}

	rr := createTurn(t, s, run.Number, "  please also add tests  ")
	if rr.Code != http.StatusAccepted {
		t.Fatalf("code = %d, want 202; body=%s", rr.Code, rr.Body.String())
	}
	var turn api.AgentTurn
	if err := json.Unmarshal(rr.Body.Bytes(), &turn); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if turn.Seq != 1 || turn.Status != "pending" || turn.Author != "agent#17" {
		t.Errorf("turn = %+v, want seq 1 / pending / agent#17", turn)
	}
	if turn.Body != "please also add tests" {
		t.Errorf("body = %q, want trimmed text", turn.Body)
	}

	stored, err := storage.ListTurns(s.db, run.ID)
	if err != nil || len(stored) != 1 {
		t.Fatalf("ListTurns = %v, %v; want one turn", stored, err)
	}
}

// Empty text is a 400; a turn on a CI run is a 400; a turn on a finished run is
// a 409.
func TestCreateAgentTurnRejections(t *testing.T) {
	s, repoID := newCITriggerServer(t)

	// CI run -> not an agent run.
	ciRun, err := storage.EnqueueRun(s.db, repoID, storage.NewRun{CommitSHA: "a", Ref: "main", Event: "push"})
	if err != nil {
		t.Fatalf("EnqueueRun ci: %v", err)
	}
	if rr := createTurn(t, s, ciRun.Number, "hi"); rr.Code != http.StatusBadRequest {
		t.Errorf("turn on CI run code = %d, want 400", rr.Code)
	}

	// Agent run, empty text -> 400.
	n := 1
	agentRun, err := storage.EnqueueRun(s.db, repoID, storage.NewRun{
		Kind: storage.RunKindAgent, IssueNumber: &n, CommitSHA: "a", Ref: "HEAD", Event: "agent",
	})
	if err != nil {
		t.Fatalf("EnqueueRun agent: %v", err)
	}
	if rr := createTurn(t, s, agentRun.Number, "   "); rr.Code != http.StatusBadRequest {
		t.Errorf("empty text code = %d, want 400", rr.Code)
	}

	// Finished agent run -> 409.
	if err := storage.FinishRun(s.db, agentRun.ID, storage.RunSuccess); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}
	if rr := createTurn(t, s, agentRun.Number, "hi"); rr.Code != http.StatusConflict {
		t.Errorf("turn on finished run code = %d, want 409", rr.Code)
	}
}

func finishRun(t *testing.T, s *Server, num int) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/repos/alice/repo/runs/"+strconv.Itoa(num)+"/finish", nil)
	req.SetPathValue("owner", "alice")
	req.SetPathValue("repo", "repo")
	req.SetPathValue("number", strconv.Itoa(num))
	req = req.WithContext(context.WithValue(req.Context(), tokenCtxKey{}, api.Token{Name: "alice"}))
	rr := httptest.NewRecorder()
	s.handleFinishAgentRun(rr, req)
	return rr
}

func cancelRun(t *testing.T, s *Server, num int) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/repos/alice/repo/runs/"+strconv.Itoa(num)+"/cancel", nil)
	req.SetPathValue("owner", "alice")
	req.SetPathValue("repo", "repo")
	req.SetPathValue("number", strconv.Itoa(num))
	req = req.WithContext(context.WithValue(req.Context(), tokenCtxKey{}, api.Token{Name: "alice"}))
	rr := httptest.NewRecorder()
	s.handleCancelAgentRun(rr, req)
	return rr
}

// fakeCanceler stands in for the runner-backed AgentCanceler in handler tests.
type fakeCanceler struct {
	ret    bool
	gotID  int64
	called bool
}

func (f *fakeCanceler) CancelAgentRun(id int64) bool {
	f.called, f.gotID = true, id
	return f.ret
}

func TestCancelAgentRun(t *testing.T) {
	enqueueAgent := func(t *testing.T, s *Server, repoID int64) storage.CIRun {
		t.Helper()
		n := 1
		run, err := storage.EnqueueRun(s.db, repoID, storage.NewRun{
			Kind: storage.RunKindAgent, IssueNumber: &n, CommitSHA: "a", Ref: "HEAD", Event: "agent",
		})
		if err != nil {
			t.Fatalf("EnqueueRun: %v", err)
		}
		return run
	}

	t.Run("cancels a running agent run -> 202", func(t *testing.T) {
		s, repoID := newCITriggerServer(t)
		run := enqueueAgent(t, s, repoID)
		fc := &fakeCanceler{ret: true}
		s.SetAgentCanceler(fc)

		rr := cancelRun(t, s, run.Number)
		if rr.Code != http.StatusAccepted {
			t.Fatalf("code = %d, want 202; body=%s", rr.Code, rr.Body.String())
		}
		if !fc.called || fc.gotID != run.ID {
			t.Errorf("canceler called=%v id=%d, want true id=%d", fc.called, fc.gotID, run.ID)
		}
		var got api.CIRun
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got.Status != "canceled" {
			t.Errorf("status = %q, want canceled", got.Status)
		}
	})

	t.Run("already-terminal -> 409", func(t *testing.T) {
		s, repoID := newCITriggerServer(t)
		run := enqueueAgent(t, s, repoID)
		s.SetAgentCanceler(&fakeCanceler{ret: false}) // CAS found nothing to cancel
		if rr := cancelRun(t, s, run.Number); rr.Code != http.StatusConflict {
			t.Errorf("code = %d, want 409", rr.Code)
		}
	})

	t.Run("no canceler wired -> 503", func(t *testing.T) {
		s, repoID := newCITriggerServer(t)
		run := enqueueAgent(t, s, repoID)
		if rr := cancelRun(t, s, run.Number); rr.Code != http.StatusServiceUnavailable {
			t.Errorf("code = %d, want 503", rr.Code)
		}
	})

	t.Run("non-agent run -> 400", func(t *testing.T) {
		s, repoID := newCITriggerServer(t)
		s.SetAgentCanceler(&fakeCanceler{ret: true})
		run, err := storage.EnqueueRun(s.db, repoID, storage.NewRun{
			Kind: storage.RunKindCI, CommitSHA: "a", Ref: "HEAD", Event: "push",
		})
		if err != nil {
			t.Fatalf("EnqueueRun: %v", err)
		}
		if rr := cancelRun(t, s, run.Number); rr.Code != http.StatusBadRequest {
			t.Errorf("code = %d, want 400", rr.Code)
		}
	})
}

// Finishing a parked agent run transitions it to finishing; finishing a run
// that isn't awaiting input is a 409.
func TestFinishAgentRun(t *testing.T) {
	s, repoID := newCITriggerServer(t)
	n := 1
	run, err := storage.EnqueueRun(s.db, repoID, storage.NewRun{
		Kind: storage.RunKindAgent, IssueNumber: &n, CommitSHA: "a", Ref: "HEAD", Event: "agent",
	})
	if err != nil {
		t.Fatalf("EnqueueRun: %v", err)
	}

	// queued (not awaiting_input) -> 409.
	if rr := finishRun(t, s, run.Number); rr.Code != http.StatusConflict {
		t.Fatalf("finish on queued code = %d, want 409", rr.Code)
	}

	// Park it, then finish -> 202 finishing.
	if _, err := storage.ClaimNextRunOfKind(s.db, storage.RunKindAgent, 0); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := storage.MarkRunAwaitingInput(s.db, run.ID); err != nil {
		t.Fatalf("park: %v", err)
	}
	rr := finishRun(t, s, run.Number)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("finish code = %d, want 202; body=%s", rr.Code, rr.Body.String())
	}
	var got api.CIRun
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != "finishing" {
		t.Errorf("status = %q, want finishing", got.Status)
	}
}

// An unresolvable base ref is a 400.
func TestSpawnAgentBadRef(t *testing.T) {
	s, repoID := newCITriggerServer(t)
	issue, err := storage.CreateIssue(s.db, repoID, api.CreateIssueRequest{Title: "x", Author: "alice"})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	rr := spawnAgent(t, s, issue.Number, "no-such-branch")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}
