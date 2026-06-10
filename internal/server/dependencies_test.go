package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/alehatsman/moongit/internal/api"
	"github.com/alehatsman/moongit/internal/config"
	"github.com/alehatsman/moongit/internal/storage"
)

// newDepServer builds a server with one repo holding three issues, enough to
// exercise the depends-on edge endpoints.
func newDepServer(t *testing.T) *Server {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "dep.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	repoID, err := storage.EnsureRepo(db, "alice", "demo")
	if err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := storage.CreateIssue(db, repoID, api.CreateIssueRequest{Title: "iss", Author: "alice"}); err != nil {
			t.Fatalf("CreateIssue: %v", err)
		}
	}
	return &Server{
		cfg:    &config.Config{ReposDir: t.TempDir(), DataDir: t.TempDir()},
		db:     db,
		rdb:    db,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func addDep(t *testing.T, s *Server, issue, dependsOn int) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(api.AddDependencyRequest{DependsOn: dependsOn})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost,
		"/api/repos/alice/demo/issues/"+strconv.Itoa(issue)+"/dependencies", bytes.NewReader(body))
	s.apiHandler().ServeHTTP(rr, req)
	return rr
}

func TestHandleAddDependencyAndGet(t *testing.T) {
	s := newDepServer(t)
	if rr := addDep(t, s, 1, 2); rr.Code != http.StatusOK {
		t.Fatalf("add dependency: status %d, body %s", rr.Code, rr.Body.String())
	}

	// GET #1 should carry depends_on=[#2]; GET #2 should carry blocks=[#1].
	rr := httptest.NewRecorder()
	s.apiHandler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/repos/alice/demo/issues/1", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("get #1: status %d", rr.Code)
	}
	var iss api.Issue
	if err := json.Unmarshal(rr.Body.Bytes(), &iss); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(iss.DependsOn) != 1 || iss.DependsOn[0].Number != 2 {
		t.Fatalf("depends_on = %+v, want [#2]", iss.DependsOn)
	}

	rr = httptest.NewRecorder()
	s.apiHandler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/repos/alice/demo/issues/2", nil))
	var iss2 api.Issue
	if err := json.Unmarshal(rr.Body.Bytes(), &iss2); err != nil {
		t.Fatalf("decode #2: %v", err)
	}
	if len(iss2.Blocks) != 1 || iss2.Blocks[0].Number != 1 {
		t.Fatalf("blocks = %+v, want [#1]", iss2.Blocks)
	}
}

func TestHandleAddDependencySelfRejected(t *testing.T) {
	s := newDepServer(t)
	if rr := addDep(t, s, 1, 1); rr.Code != http.StatusBadRequest {
		t.Fatalf("self-dependency: status %d, want 400", rr.Code)
	}
}

func TestHandleAddDependencyCycleRejected(t *testing.T) {
	s := newDepServer(t)
	if rr := addDep(t, s, 1, 2); rr.Code != http.StatusOK {
		t.Fatalf("1->2: status %d", rr.Code)
	}
	if rr := addDep(t, s, 2, 1); rr.Code != http.StatusConflict {
		t.Fatalf("2->1 (cycle): status %d, want 409", rr.Code)
	}
}

func TestHandleAddDependencyMissingTarget(t *testing.T) {
	s := newDepServer(t)
	if rr := addDep(t, s, 1, 9); rr.Code != http.StatusBadRequest {
		t.Fatalf("missing target: status %d, want 400", rr.Code)
	}
}

func TestHandleRemoveDependency(t *testing.T) {
	s := newDepServer(t)
	if rr := addDep(t, s, 1, 2); rr.Code != http.StatusOK {
		t.Fatalf("add: status %d", rr.Code)
	}
	rr := httptest.NewRecorder()
	s.apiHandler().ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/api/repos/alice/demo/issues/1/dependencies/2", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("remove: status %d, body %s", rr.Code, rr.Body.String())
	}
	var iss api.Issue
	if err := json.Unmarshal(rr.Body.Bytes(), &iss); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(iss.DependsOn) != 0 {
		t.Fatalf("depends_on = %+v after remove, want empty", iss.DependsOn)
	}
}
