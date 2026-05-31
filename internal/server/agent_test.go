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
