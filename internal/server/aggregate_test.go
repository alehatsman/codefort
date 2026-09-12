package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/alehatsman/codefort/internal/api"
	"github.com/alehatsman/codefort/internal/config"
	"github.com/alehatsman/codefort/internal/storage"
)

// newAggregateServer builds a server backed by a temp DB holding two repos
// owned by different users, with one issue and one CI run in each — enough to
// prove the cross-repo aggregate endpoints span repos and tag each row.
func newAggregateServer(t *testing.T) *Server {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "agg.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	alice, err := storage.EnsureRepo(db, "alice", "demo")
	if err != nil {
		t.Fatalf("EnsureRepo alice: %v", err)
	}
	bob, err := storage.EnsureRepo(db, "bob", "api")
	if err != nil {
		t.Fatalf("EnsureRepo bob: %v", err)
	}
	if _, err := storage.CreateIssue(db, alice, api.CreateIssueRequest{Title: "alice issue", Author: "alice"}); err != nil {
		t.Fatalf("CreateIssue alice: %v", err)
	}
	if _, err := storage.CreateIssue(db, bob, api.CreateIssueRequest{Title: "bob issue", Author: "bob"}); err != nil {
		t.Fatalf("CreateIssue bob: %v", err)
	}
	if _, err := storage.EnqueueRun(db, alice, storage.NewRun{Kind: storage.RunKindCI, CommitSHA: "a1", Ref: "refs/heads/main", Event: "push"}); err != nil {
		t.Fatalf("EnqueueRun alice: %v", err)
	}

	return &Server{
		cfg:    &config.Config{ReposDir: t.TempDir(), DataDir: t.TempDir()},
		db:     db,
		rdb:    db,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestHandleListAllIssuesSpansReposWithRepoRef(t *testing.T) {
	s := newAggregateServer(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/issues", nil)
	s.apiHandler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", rr.Code, rr.Body.String())
	}
	var got []api.IssueWithRepo
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d issues, want 2 (one per repo)", len(got))
	}
	repos := map[string]bool{}
	for _, iss := range got {
		repos[iss.Repo.Owner+"/"+iss.Repo.Name] = true
	}
	if !repos["alice/demo"] || !repos["bob/api"] {
		t.Errorf("repos = %v, want both alice/demo and bob/api", repos)
	}
}

func TestHandleListAllRunsKindFilterRoutes(t *testing.T) {
	s := newAggregateServer(t)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/runs?kind=ci", nil)
	s.apiHandler().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", rr.Code, rr.Body.String())
	}
	var got []api.CIRunWithRepo
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0].Repo.Owner != "alice" || got[0].Kind != "ci" {
		t.Fatalf("ci runs => %+v, want alice/demo's single ci run", got)
	}
}

// TestHandleListAllRunsStateFilterRoutes proves the ?state query param reaches
// storage.ListAllRuns. The seed enqueues one CI run, which starts queued, so a
// matching state returns it and a non-matching state returns none.
func TestHandleListAllRunsStateFilterRoutes(t *testing.T) {
	s := newAggregateServer(t)
	runsFor := func(query string) []api.CIRunWithRepo {
		t.Helper()
		rr := httptest.NewRecorder()
		s.apiHandler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/runs"+query, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d (body=%s)", rr.Code, rr.Body.String())
		}
		var got []api.CIRunWithRepo
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return got
	}

	if got := runsFor("?state=queued,running"); len(got) != 1 || got[0].Status != "queued" {
		t.Fatalf("?state=queued,running => %+v, want the single queued run", got)
	}
	if got := runsFor("?state=success"); len(got) != 0 {
		t.Fatalf("?state=success => %+v, want none", got)
	}
}
