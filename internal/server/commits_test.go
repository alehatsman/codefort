package server

import (
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

func TestParseCommits(t *testing.T) {
	// Two records: the first carries a multi-line body, the second is
	// single-line (empty body field). US (\x1f) separates fields; RS (\x1e)
	// terminates each record; git emits a trailing newline between records.
	raw := "" +
		"abc123def\x1fabc123d\x1fAlice\x1fa@b.c\x1f2026-05-30T10:00:00+02:00\x1ffix the thing\x1fbody line one\nbody line two\x1e\n" +
		"99887766\x1f9988776\x1fBob\x1fbob@x.y\x1f2026-05-29T09:00:00Z\x1finitial commit\x1f\x1e\n"

	got := parseCommits([]byte(raw))
	if len(got) != 2 {
		t.Fatalf("got %d commits, want 2", len(got))
	}

	if got[0].SHA != "abc123def" || got[0].ShortSHA != "abc123d" {
		t.Errorf("commit[0] sha = %q/%q", got[0].SHA, got[0].ShortSHA)
	}
	if got[0].Author != "Alice" || got[0].Email != "a@b.c" {
		t.Errorf("commit[0] author = %q <%q>", got[0].Author, got[0].Email)
	}
	if got[0].Subject != "fix the thing" {
		t.Errorf("commit[0] subject = %q", got[0].Subject)
	}
	if got[0].Body != "body line one\nbody line two" {
		t.Errorf("commit[0] body = %q", got[0].Body)
	}
	if got[0].Date.IsZero() {
		t.Errorf("commit[0] date not parsed")
	}
	if got[1].Subject != "initial commit" || got[1].Body != "" {
		t.Errorf("commit[1] subject=%q body=%q", got[1].Subject, got[1].Body)
	}
}

func TestParseCommitsMalformed(t *testing.T) {
	// A record with too few fields is skipped; a well-formed one survives.
	raw := "garbage-no-separators\x1e\n" +
		"sha\x1fsh\x1fA\x1fa@b\x1f2026-05-30T10:00:00Z\x1fsubject\x1f\x1e\n"
	got := parseCommits([]byte(raw))
	if len(got) != 1 {
		t.Fatalf("got %d commits, want 1", len(got))
	}
	if got[0].Subject != "subject" {
		t.Errorf("subject = %q", got[0].Subject)
	}
}

// --- integration: real bare repo with a known commit sequence ---

// commit sequence (oldest -> newest):
//
//	c1 "first commit"  adds a.txt, dir/b.txt
//	c2 "edit a"        modifies a.txt (carries a multi-line body)
//	c3 "add c"         adds c.txt
const (
	cOwner = "alice"
	cRepo  = "proj"
)

func newCommitTestServer(t *testing.T) *Server {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := storage.EnsureRepo(db, cOwner, cRepo); err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}

	reposDir := t.TempDir()
	work := t.TempDir()
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Alice", "GIT_AUTHOR_EMAIL=a@b.c",
			"GIT_COMMITTER_NAME=Alice", "GIT_COMMITTER_EMAIL=a@b.c",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(work, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	git(work, "init", "-q", "-b", "main")
	write("a.txt", "one")
	write("dir/b.txt", "one")
	git(work, "add", ".")
	git(work, "commit", "-q", "-m", "first commit")
	write("a.txt", "two")
	git(work, "add", ".")
	git(work, "commit", "-q", "-m", "edit a", "-m", "body line one\nbody line two")
	write("c.txt", "three")
	git(work, "add", ".")
	git(work, "commit", "-q", "-m", "add c")

	bare := filepath.Join(reposDir, cOwner, cRepo+".git")
	if err := os.MkdirAll(filepath.Dir(bare), 0o755); err != nil {
		t.Fatal(err)
	}
	git(work, "clone", "-q", "--bare", work, bare)

	return &Server{
		cfg:    &config.Config{ReposDir: reposDir},
		db:     db,
		rdb:    db,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func getJSON[T any](t *testing.T, s *Server, h http.HandlerFunc, target string) (T, int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.SetPathValue("owner", cOwner)
	req.SetPathValue("repo", cRepo)
	rr := httptest.NewRecorder()
	h(rr, req)
	var out T
	if rr.Code == http.StatusOK {
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode %s: %v (body=%s)", target, err, rr.Body.String())
		}
	}
	return out, rr.Code
}

func TestHandleCommits(t *testing.T) {
	s := newCommitTestServer(t)

	// Whole-repo history, newest first.
	out, code := getJSON[api.CommitList](t, s, s.handleCommits, "/api/repos/alice/proj/commits")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if len(out.Commits) != 3 {
		t.Fatalf("got %d commits, want 3", len(out.Commits))
	}
	wantSubjects := []string{"add c", "edit a", "first commit"}
	for i, want := range wantSubjects {
		if out.Commits[i].Subject != want {
			t.Errorf("commit[%d] subject = %q, want %q", i, out.Commits[i].Subject, want)
		}
	}
	if out.Ref != "main" {
		t.Errorf("ref = %q, want main", out.Ref)
	}
	// The middle commit carries a body.
	if out.Commits[1].Body != "body line one\nbody line two" {
		t.Errorf("edit-a body = %q", out.Commits[1].Body)
	}
	if out.HasMore {
		t.Errorf("HasMore = true, want false")
	}
}

func TestHandleCommitsPagination(t *testing.T) {
	s := newCommitTestServer(t)

	p1, _ := getJSON[api.CommitList](t, s, s.handleCommits, "/api/repos/alice/proj/commits?per_page=2&page=1")
	if len(p1.Commits) != 2 || !p1.HasMore {
		t.Fatalf("page1: got %d commits hasMore=%v, want 2 true", len(p1.Commits), p1.HasMore)
	}
	if p1.Commits[0].Subject != "add c" || p1.Commits[1].Subject != "edit a" {
		t.Errorf("page1 subjects = %q, %q", p1.Commits[0].Subject, p1.Commits[1].Subject)
	}

	p2, _ := getJSON[api.CommitList](t, s, s.handleCommits, "/api/repos/alice/proj/commits?per_page=2&page=2")
	if len(p2.Commits) != 1 || p2.HasMore {
		t.Fatalf("page2: got %d commits hasMore=%v, want 1 false", len(p2.Commits), p2.HasMore)
	}
	if p2.Commits[0].Subject != "first commit" {
		t.Errorf("page2 subject = %q", p2.Commits[0].Subject)
	}
}

func TestHandleCommitsPathFilter(t *testing.T) {
	s := newCommitTestServer(t)

	// a.txt was touched by c1 (add) and c2 (edit), not c3.
	out, _ := getJSON[api.CommitList](t, s, s.handleCommits, "/api/repos/alice/proj/commits?path=a.txt")
	if len(out.Commits) != 2 {
		t.Fatalf("got %d commits for a.txt, want 2", len(out.Commits))
	}
	if out.Commits[0].Subject != "edit a" || out.Commits[1].Subject != "first commit" {
		t.Errorf("a.txt subjects = %q, %q", out.Commits[0].Subject, out.Commits[1].Subject)
	}
}

func TestHandleTreeCommits(t *testing.T) {
	s := newCommitTestServer(t)

	out, code := getJSON[api.TreeCommits](t, s, s.handleTreeCommits, "/api/repos/alice/proj/tree-commits")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if out.Total != 3 {
		t.Errorf("total = %d, want 3", out.Total)
	}
	if out.Latest == nil || out.Latest.Subject != "add c" {
		t.Fatalf("latest = %+v, want subject 'add c'", out.Latest)
	}
	// Per-child last-touching commit.
	want := map[string]string{
		"a.txt": "edit a",
		"dir":   "first commit",
		"c.txt": "add c",
	}
	for path, subj := range want {
		got, ok := out.Entries[path]
		if !ok {
			t.Errorf("missing entry for %q", path)
			continue
		}
		if got.Subject != subj {
			t.Errorf("entry %q subject = %q, want %q", path, got.Subject, subj)
		}
	}
}

func TestHandleCommitsUnbornRepo(t *testing.T) {
	// A registered repo with a bare dir but no commits returns empty, not 500.
	db, err := storage.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := storage.EnsureRepo(db, cOwner, cRepo); err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}
	reposDir := t.TempDir()
	bare := filepath.Join(reposDir, cOwner, cRepo+".git")
	if out, err := exec.Command("git", "init", "-q", "--bare", "-b", "main", bare).CombinedOutput(); err != nil {
		t.Fatalf("git init bare: %v: %s", err, out)
	}
	s := &Server{
		cfg:    &config.Config{ReposDir: reposDir},
		db:     db,
		rdb:    db,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	out, code := getJSON[api.CommitList](t, s, s.handleCommits, "/api/repos/alice/proj/commits")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if len(out.Commits) != 0 {
		t.Errorf("got %d commits, want 0", len(out.Commits))
	}

	tc, _ := getJSON[api.TreeCommits](t, s, s.handleTreeCommits, "/api/repos/alice/proj/tree-commits")
	if tc.Latest != nil || len(tc.Entries) != 0 {
		t.Errorf("unborn tree-commits: latest=%+v entries=%d", tc.Latest, len(tc.Entries))
	}
}
