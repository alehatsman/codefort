package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/alehatsman/moongit/internal/api"
	"github.com/alehatsman/moongit/internal/config"
	"github.com/alehatsman/moongit/internal/storage"
)

// newCITriggerServer builds a server backed by a real bare repo (one commit on
// main, tagged v1) so handleTriggerCIRun's git ref resolution exercises actual
// rev-parse. CI is left disabled — callers enable it as the test needs.
func newCITriggerServer(t *testing.T) (*Server, int64) {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "ci.db"))
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

	reposDir := t.TempDir()
	work := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = work
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Alice", "GIT_AUTHOR_EMAIL=a@b.c",
			"GIT_COMMITTER_NAME=Alice", "GIT_COMMITTER_EMAIL=a@b.c",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(work, "f.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-q", "-m", "init")
	git("tag", "v1")

	bare := filepath.Join(reposDir, "alice", "repo.git")
	if err := os.MkdirAll(filepath.Dir(bare), 0o755); err != nil {
		t.Fatal(err)
	}
	git("clone", "-q", "--bare", work, bare)

	s := &Server{
		cfg:    &config.Config{ReposDir: reposDir},
		db:     db,
		rdb:    db,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return s, repoID
}

// trigger drives handleTriggerCIRun directly with owner/repo path values and an
// authenticated identity, the same way code_comments_test.go's call helper does.
func trigger(t *testing.T, s *Server, ref string) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(api.TriggerCIRunRequest{Ref: ref})
	req := httptest.NewRequest(http.MethodPost, "/api/repos/alice/repo/runs", bytes.NewReader(b))
	req.SetPathValue("owner", "alice")
	req.SetPathValue("repo", "repo")
	req = req.WithContext(context.WithValue(req.Context(), tokenCtxKey{}, api.Token{Name: "agent#17"}))
	rr := httptest.NewRecorder()
	s.handleTriggerCIRun(rr, req)
	return rr
}

func TestTriggerCIRunByBranch(t *testing.T) {
	s, repoID := newCITriggerServer(t)
	if err := storage.SetRepoCIEnabled(s.db, repoID, true); err != nil {
		t.Fatalf("SetRepoCIEnabled: %v", err)
	}

	rr := trigger(t, s, "main")
	if rr.Code != http.StatusAccepted {
		t.Fatalf("code = %d, want 202; body=%s", rr.Code, rr.Body.String())
	}
	var run api.CIRun
	if err := json.Unmarshal(rr.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	if run.Event != "manual" {
		t.Errorf("event = %q, want manual", run.Event)
	}
	if run.Trigger != "agent#17" {
		t.Errorf("trigger = %q, want agent#17", run.Trigger)
	}
	if run.Ref != "main" {
		t.Errorf("ref = %q, want main", run.Ref)
	}
	if len(run.CommitSHA) < 40 {
		t.Errorf("commit_sha = %q, want a resolved sha", run.CommitSHA)
	}

	// The run is actually enqueued and waiting for the runner.
	got, err := storage.GetRun(s.db, repoID, run.Number)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Status != storage.RunQueued {
		t.Errorf("status = %q, want queued", got.Status)
	}
}

func TestTriggerCIRunByTagResolvesToCommit(t *testing.T) {
	s, repoID := newCITriggerServer(t)
	if err := storage.SetRepoCIEnabled(s.db, repoID, true); err != nil {
		t.Fatalf("SetRepoCIEnabled: %v", err)
	}

	main := trigger(t, s, "main")
	tag := trigger(t, s, "v1")
	if main.Code != http.StatusAccepted || tag.Code != http.StatusAccepted {
		t.Fatalf("codes: main=%d tag=%d", main.Code, tag.Code)
	}
	var mr, tr api.CIRun
	_ = json.Unmarshal(main.Body.Bytes(), &mr)
	_ = json.Unmarshal(tag.Body.Bytes(), &tr)
	if tr.CommitSHA != mr.CommitSHA {
		t.Errorf("tag v1 resolved to %q, want main's %q", tr.CommitSHA, mr.CommitSHA)
	}
}

func TestTriggerCIRunUnknownRef(t *testing.T) {
	s, repoID := newCITriggerServer(t)
	if err := storage.SetRepoCIEnabled(s.db, repoID, true); err != nil {
		t.Fatalf("SetRepoCIEnabled: %v", err)
	}
	rr := trigger(t, s, "no-such-ref")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestTriggerCIRunRejectsDashRef(t *testing.T) {
	s, repoID := newCITriggerServer(t)
	if err := storage.SetRepoCIEnabled(s.db, repoID, true); err != nil {
		t.Fatalf("SetRepoCIEnabled: %v", err)
	}
	// A leading-dash ref must be refused before it reaches git as a flag.
	rr := trigger(t, s, "--output=/etc/passwd")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestTriggerCIRunDisabledRepo(t *testing.T) {
	s, _ := newCITriggerServer(t) // CI left disabled
	rr := trigger(t, s, "main")
	if rr.Code != http.StatusConflict {
		t.Fatalf("code = %d, want 409; body=%s", rr.Code, rr.Body.String())
	}
}
