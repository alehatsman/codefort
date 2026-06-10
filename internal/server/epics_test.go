package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alehatsman/moongit/internal/api"
	"github.com/alehatsman/moongit/internal/storage"
)

// makeChild reparents an existing issue under a parent epic via the storage
// layer, so the server tests can build an epic without driving PATCH.
func makeChild(t *testing.T, s *Server, repoID int64, child, parent int) {
	t.Helper()
	if _, err := storage.UpdateIssue(s.db, repoID, child, nil, nil, nil, &parent, nil); err != nil {
		t.Fatalf("set parent %d->%d: %v", child, parent, err)
	}
}

func TestHandleListEpicsCarriesProgress(t *testing.T) {
	s := newDepServer(t) // issues 1,2,3
	repoID, err := storage.LookupRepo(s.rdb, "alice", "demo")
	if err != nil {
		t.Fatalf("LookupRepo: %v", err)
	}
	// #1 becomes an epic with children #2, #3; close #2.
	makeChild(t, s, repoID, 2, 1)
	makeChild(t, s, repoID, 3, 1)
	closed := api.IssueClosed
	if _, err := storage.UpdateIssue(s.db, repoID, 2, &closed, nil, nil, nil, nil); err != nil {
		t.Fatalf("close #2: %v", err)
	}

	rr := httptest.NewRecorder()
	s.apiHandler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/repos/alice/demo/issues?epics=1", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("list epics: status %d", rr.Code)
	}
	var issues []api.Issue
	if err := json.Unmarshal(rr.Body.Bytes(), &issues); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(issues) != 1 || issues[0].Number != 1 {
		t.Fatalf("epics list = %+v, want [#1]", issues)
	}
	if issues[0].Progress == nil || issues[0].Progress.Total != 2 || issues[0].Progress.Done != 1 {
		t.Fatalf("progress = %+v, want 1/2", issues[0].Progress)
	}
}

func TestHandleGetIssueCarriesProgress(t *testing.T) {
	s := newDepServer(t)
	repoID, _ := storage.LookupRepo(s.rdb, "alice", "demo")
	makeChild(t, s, repoID, 2, 1)
	makeChild(t, s, repoID, 3, 1)

	rr := httptest.NewRecorder()
	s.apiHandler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/repos/alice/demo/issues/1", nil))
	var iss api.Issue
	if err := json.Unmarshal(rr.Body.Bytes(), &iss); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if iss.Progress == nil || iss.Progress.Total != 2 || iss.Progress.Done != 0 {
		t.Fatalf("progress = %+v, want 0/2", iss.Progress)
	}
	// A leaf issue carries no progress.
	rr = httptest.NewRecorder()
	s.apiHandler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/repos/alice/demo/issues/2", nil))
	var leaf api.Issue
	if err := json.Unmarshal(rr.Body.Bytes(), &leaf); err != nil {
		t.Fatalf("decode leaf: %v", err)
	}
	if leaf.Progress != nil {
		t.Fatalf("leaf progress = %+v, want nil", leaf.Progress)
	}
}

func TestHandleListEpicsRejectsReadyCombo(t *testing.T) {
	s := newDepServer(t)
	rr := httptest.NewRecorder()
	s.apiHandler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/repos/alice/demo/issues?epics=1&ready=1", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("epics+ready: status %d, want 400", rr.Code)
	}
}
