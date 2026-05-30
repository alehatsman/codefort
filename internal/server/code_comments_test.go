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
	"strconv"
	"testing"

	"github.com/alehatsman/moongit/internal/api"
	"github.com/alehatsman/moongit/internal/config"
	"github.com/alehatsman/moongit/internal/storage"
)

// newCodeCommentTestServer builds a bare repo with two branches:
//
//	main:    a.go has 5 lines ("la1".."la5")
//	feature: a.go line 2 changed to "feature-2"
func newCodeCommentTestServer(t *testing.T) *Server {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "cc.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := storage.EnsureRepo(db, "alice", "proj"); err != nil {
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
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(work, "a.go"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	git("init", "-q", "-b", "main")
	write("la1\nla2\nla3\nla4\nla5\n")
	git("add", ".")
	git("commit", "-q", "-m", "init")
	git("checkout", "-q", "-b", "feature")
	write("la1\nfeature-2\nla3\nla4\nla5\n")
	git("add", ".")
	git("commit", "-q", "-m", "feature edit")
	git("checkout", "-q", "main")

	bare := filepath.Join(reposDir, "alice", "proj.git")
	if err := os.MkdirAll(filepath.Dir(bare), 0o755); err != nil {
		t.Fatal(err)
	}
	git("clone", "-q", "--bare", work, bare)

	return &Server{
		cfg:    &config.Config{ReposDir: reposDir},
		db:     db,
		rdb:    db,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// call invokes a handler with owner/repo path values and an identity stamped
// on the context (empty author => no token).
func call(t *testing.T, s *Server, h http.HandlerFunc, method, target, author string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, target, r)
	req.SetPathValue("owner", "alice")
	req.SetPathValue("repo", "proj")
	if author != "" {
		req = req.WithContext(context.WithValue(req.Context(), tokenCtxKey{}, api.Token{Name: author}))
	}
	rr := httptest.NewRecorder()
	h(rr, req)
	return rr
}

func TestHandleListRefs(t *testing.T) {
	s := newCodeCommentTestServer(t)
	rr := call(t, s, s.handleListRefs, http.MethodGet, "/api/repos/alice/proj/refs", "", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body)
	}
	var out api.RefList
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Default != "main" {
		t.Errorf("default = %q, want main", out.Default)
	}
	if len(out.Branches) != 2 || out.Branches[0] != "feature" || out.Branches[1] != "main" {
		t.Errorf("branches = %v, want [feature main]", out.Branches)
	}
}

func TestBlobRefThreading(t *testing.T) {
	s := newCodeCommentTestServer(t)

	main, _ := getJSON[api.Blob](t, s, s.handleBlob, "/api/repos/alice/proj/blob?path=a.go")
	if main.Ref != "main" || main.Content != "la1\nla2\nla3\nla4\nla5\n" {
		t.Errorf("main blob ref=%q content=%q", main.Ref, main.Content)
	}

	feat, _ := getJSON[api.Blob](t, s, s.handleBlob, "/api/repos/alice/proj/blob?path=a.go&ref=feature")
	if feat.Ref != "feature" || feat.Content != "la1\nfeature-2\nla3\nla4\nla5\n" {
		t.Errorf("feature blob ref=%q content=%q", feat.Ref, feat.Content)
	}

	_, code := getJSON[api.Blob](t, s, s.handleBlob, "/api/repos/alice/proj/blob?path=a.go&ref=nope")
	if code != http.StatusNotFound {
		t.Errorf("bad ref status = %d, want 404", code)
	}
}

func TestCodeCommentCreateListSnippet(t *testing.T) {
	s := newCodeCommentTestServer(t)

	// Create a comment on lines 2-3 of main's a.go.
	rr := call(t, s, s.handleCreateCodeComment, http.MethodPost, "/api/repos/alice/proj/code-comments", "alice",
		api.CreateCodeCommentRequest{Ref: "main", Path: "a.go", StartLine: 2, EndLine: 3, Body: "look here"})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%s", rr.Code, rr.Body)
	}
	var created api.CodeComment
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Author != "alice" {
		t.Errorf("author = %q, want alice (stamped from token)", created.Author)
	}
	if created.CommitSha == "" {
		t.Errorf("commit_sha not stamped")
	}

	// List on main: snippet is the referenced source lines.
	list, _ := getJSON[[]api.CodeComment](t, s, s.handleListCodeComments, "/api/repos/alice/proj/code-comments?ref=main")
	if len(list) != 1 {
		t.Fatalf("got %d comments, want 1", len(list))
	}
	if list[0].Snippet != "la2\nla3" {
		t.Errorf("snippet = %q, want %q", list[0].Snippet, "la2\nla3")
	}

	// The comment is bound to main: feature's listing is empty.
	featList, _ := getJSON[[]api.CodeComment](t, s, s.handleListCodeComments, "/api/repos/alice/proj/code-comments?ref=feature")
	if len(featList) != 0 {
		t.Errorf("feature listing = %d, want 0 (branch-bound)", len(featList))
	}
}

func TestCodeCommentResolveAndDeleteHandlers(t *testing.T) {
	s := newCodeCommentTestServer(t)
	c, err := storage.CreateCodeComment(s.db, mustRepoID(t, s), api.CreateCodeCommentRequest{
		Ref: "main", Path: "a.go", StartLine: 1, EndLine: 1, Author: "alice", Body: "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(c.ID, 10)
	target := "/api/repos/alice/proj/code-comments/" + id

	// Non-author cannot resolve.
	rr := callWithID(t, s, s.handlePatchCodeComment, http.MethodPatch, target, "bob", id,
		api.UpdateCodeCommentRequest{Resolved: ptr(true)})
	if rr.Code != http.StatusForbidden {
		t.Errorf("non-author resolve = %d, want 403", rr.Code)
	}

	// Author resolves.
	rr = callWithID(t, s, s.handlePatchCodeComment, http.MethodPatch, target, "alice", id,
		api.UpdateCodeCommentRequest{Resolved: ptr(true)})
	if rr.Code != http.StatusOK {
		t.Fatalf("resolve = %d, body=%s", rr.Code, rr.Body)
	}

	// Non-author cannot delete; author can.
	if rr := callWithID(t, s, s.handleDeleteCodeComment, http.MethodDelete, target, "bob", id, nil); rr.Code != http.StatusForbidden {
		t.Errorf("non-author delete = %d, want 403", rr.Code)
	}
	if rr := callWithID(t, s, s.handleDeleteCodeComment, http.MethodDelete, target, "alice", id, nil); rr.Code != http.StatusNoContent {
		t.Fatalf("delete = %d, body=%s", rr.Code, rr.Body)
	}
}

// callWithID is call() plus the {id} path value the comment handlers read.
func callWithID(t *testing.T, s *Server, h http.HandlerFunc, method, target, author, id string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, target, r)
	req.SetPathValue("owner", "alice")
	req.SetPathValue("repo", "proj")
	req.SetPathValue("id", id)
	if author != "" {
		req = req.WithContext(context.WithValue(req.Context(), tokenCtxKey{}, api.Token{Name: author}))
	}
	rr := httptest.NewRecorder()
	h(rr, req)
	return rr
}

func mustRepoID(t *testing.T, s *Server) int64 {
	t.Helper()
	id, err := storage.LookupRepo(s.rdb, "alice", "proj")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func ptr[T any](v T) *T { return &v }
