package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/alehatsman/codefort/internal/api"
	"github.com/alehatsman/codefort/internal/ci"
	"github.com/alehatsman/codefort/internal/config"
	"github.com/alehatsman/codefort/internal/storage"
)

// newRepoDeleteServer builds a server backed by a temp DB and repos dir, with
// one registered repo ("alice/repo") whose bare git dir exists on disk.
func newRepoDeleteServer(t *testing.T) (*Server, string) {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "repos.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := storage.EnsureRepo(db, "alice", "repo"); err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}

	reposDir := t.TempDir()
	gitDir := filepath.Join(reposDir, "alice", "repo.git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatalf("mkdir git dir: %v", err)
	}

	s := &Server{
		cfg:    &config.Config{ReposDir: reposDir, DataDir: t.TempDir()},
		db:     db,
		rdb:    db,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return s, gitDir
}

// deleteRepo routes a DELETE through the mux so {owner}/{repo} resolve, with a
// token mounted on the context the way withAuth would.
func deleteRepo(s *Server, tok api.Token, owner, repo string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/repos/"+owner+"/"+repo, nil)
	req = req.WithContext(context.WithValue(req.Context(), tokenCtxKey{}, tok))
	s.apiHandler().ServeHTTP(rr, req)
	return rr
}

func TestDeleteRepoHandlerRemovesRowAndGitDir(t *testing.T) {
	s, gitDir := newRepoDeleteServer(t)
	tok, _ := storage.CreateToken(s.db, "alice", "mgt_alice")

	rr := deleteRepo(s, tok, "alice", "repo")
	if rr.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body %s", rr.Code, rr.Body.String())
	}

	// DB row gone.
	if _, err := storage.LookupRepo(s.db, "alice", "repo"); err != storage.ErrNotFound {
		t.Errorf("LookupRepo after delete = %v, want ErrNotFound", err)
	}
	// Git dir gone, and the now-empty owner namespace dir pruned.
	if _, err := os.Stat(gitDir); !os.IsNotExist(err) {
		t.Errorf("git dir still present: stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.cfg.ReposDir, "alice")); !os.IsNotExist(err) {
		t.Errorf("empty owner dir not pruned: stat err = %v", err)
	}

	// A repo.deleted event survives the cascade (emitted with no repo_id).
	events, err := storage.ListEventsSince(s.db, 0, 0, 100)
	if err != nil {
		t.Fatalf("ListEventsSince: %v", err)
	}
	var found bool
	for _, ev := range events {
		if ev.Type == "repo.deleted" {
			found = true
		}
	}
	if !found {
		t.Errorf("no repo.deleted event found; got %d events", len(events))
	}

	// Deleting again is a clean 404.
	rr = deleteRepo(s, tok, "alice", "repo")
	if rr.Code != http.StatusNotFound {
		t.Errorf("second delete status = %d, want 404", rr.Code)
	}
}

// A repo with an in-flight (non-terminal) run can't be deleted: the cascade
// would yank the run row from under a live runner goroutine. The handler 409s
// and leaves the repo intact until the run reaches a terminal state.
func TestDeleteRepoRejectsWhileRunActive(t *testing.T) {
	s, _ := newRepoDeleteServer(t)
	tok, _ := storage.CreateToken(s.db, "alice", "mgt_alice")
	repoID, _ := storage.LookupRepo(s.db, "alice", "repo")

	run, err := storage.EnqueueRun(s.db, repoID, storage.NewRun{
		CommitSHA: "a", Ref: "refs/heads/main", Event: "push",
	})
	if err != nil {
		t.Fatalf("EnqueueRun: %v", err)
	}

	// Queued run → 409, repo untouched.
	rr := deleteRepo(s, tok, "alice", "repo")
	if rr.Code != http.StatusConflict {
		t.Fatalf("delete with active run = %d, want 409; body %s", rr.Code, rr.Body.String())
	}
	if _, err := storage.LookupRepo(s.db, "alice", "repo"); err != nil {
		t.Errorf("repo gone after a 409 delete: %v", err)
	}

	// Once the run is terminal, the delete goes through.
	if err := storage.FinishRun(s.db, run.ID, storage.RunSuccess); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}
	if rr := deleteRepo(s, tok, "alice", "repo"); rr.Code != http.StatusNoContent {
		t.Fatalf("delete after run finished = %d, want 204; body %s", rr.Code, rr.Body.String())
	}
}

// Deleting a repo removes its CI/agent event-log tree under the data dir, which
// lives outside ReposDir and so isn't covered by the git-dir removal.
func TestDeleteRepoRemovesLogTree(t *testing.T) {
	s, _ := newRepoDeleteServer(t)
	tok, _ := storage.CreateToken(s.db, "alice", "mgt_alice")

	logDir := ci.RepoLogDir(s.cfg.DataDir, "alice", "repo")
	if err := os.MkdirAll(filepath.Join(logDir, "1"), 0o755); err != nil {
		t.Fatalf("mkdir log dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(logDir, "1", "build.events.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write log file: %v", err)
	}

	if rr := deleteRepo(s, tok, "alice", "repo"); rr.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body %s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(logDir); !os.IsNotExist(err) {
		t.Errorf("log tree still present: stat err = %v", err)
	}
	// The now-empty owner log namespace is pruned too.
	if _, err := os.Stat(filepath.Join(s.cfg.DataDir, "ci", "alice")); !os.IsNotExist(err) {
		t.Errorf("empty owner log dir not pruned: stat err = %v", err)
	}
}

// An owner dir that still holds other repos must not be pruned when one of its
// repos is deleted.
func TestDeleteRepoKeepsNonEmptyOwnerDir(t *testing.T) {
	s, _ := newRepoDeleteServer(t)
	tok, _ := storage.CreateToken(s.db, "alice", "mgt_alice")

	// A second repo under the same owner, on disk only (the delete only
	// touches the filesystem prune path here).
	sibling := filepath.Join(s.cfg.ReposDir, "alice", "other.git")
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatalf("mkdir sibling: %v", err)
	}

	if rr := deleteRepo(s, tok, "alice", "repo"); rr.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body %s", rr.Code, rr.Body.String())
	}

	if _, err := os.Stat(sibling); err != nil {
		t.Errorf("sibling repo dir removed or unreadable: %v", err)
	}
}
