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
	"strings"
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

// --- commit diff endpoint ---

// getCommitDetail drives handleCommit for a given sha.
func getCommitDetail(t *testing.T, s *Server, sha string) (api.CommitDetail, int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/repos/"+cOwner+"/"+cRepo+"/commit/"+sha, nil)
	req.SetPathValue("owner", cOwner)
	req.SetPathValue("repo", cRepo)
	req.SetPathValue("sha", sha)
	rr := httptest.NewRecorder()
	s.handleCommit(rr, req)
	var out api.CommitDetail
	if rr.Code == http.StatusOK {
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode commit %s: %v (body=%s)", sha, err, rr.Body.String())
		}
	}
	return out, rr.Code
}

// newDiffTestServer builds a bare repo exercising every diff status plus a
// root commit and a merge, and returns the server alongside a label->sha map.
func newDiffTestServer(t *testing.T) (*Server, map[string]string) {
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
	env := append(os.Environ(),
		"GIT_AUTHOR_NAME=Alice", "GIT_AUTHOR_EMAIL=a@b.c",
		"GIT_COMMITTER_NAME=Alice", "GIT_COMMITTER_EMAIL=a@b.c",
	)
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = work
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
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
	head := func() string {
		t.Helper()
		return git("rev-parse", "HEAD")
	}

	shas := map[string]string{}
	git("init", "-q", "-b", "main")

	// Root commit: adds two text files.
	write("a.txt", "one\ntwo\nthree\n")
	write("dir/keep.txt", "keep\n")
	git("add", ".")
	git("commit", "-q", "-m", "root commit")
	shas["root"] = head()

	// Modify a.txt (one add, one del).
	write("a.txt", "one\nTWO\nthree\n")
	git("add", ".")
	git("commit", "-q", "-m", "modify a")
	shas["modify"] = head()

	// Delete dir/keep.txt.
	if err := os.Remove(filepath.Join(work, "dir/keep.txt")); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-q", "-m", "delete keep")
	shas["delete"] = head()

	// Add a binary file (embedded NUL).
	write("logo.bin", "PNG\x00\x01\x02binary\x00data")
	git("add", ".")
	git("commit", "-q", "-m", "add binary")
	shas["binary"] = head()

	// Rename a.txt -> renamed.txt verbatim (pure rename, detected by -M).
	git("mv", "a.txt", "renamed.txt")
	git("commit", "-q", "-m", "rename a")
	shas["rename"] = head()

	// Merge: branch off, commit, merge back with a real merge commit.
	git("checkout", "-q", "-b", "feature")
	write("feature.txt", "feature\n")
	git("add", ".")
	git("commit", "-q", "-m", "feature work")
	git("checkout", "-q", "main")
	// Touch a file on main so the merge isn't a fast-forward. The merge's first
	// parent is this commit (main's tip at merge time).
	write("renamed.txt", "one\nTWO\nthree\nmain-edit\n")
	git("add", ".")
	git("commit", "-q", "-m", "main edit")
	shas["mainTip"] = head()
	git("merge", "-q", "--no-ff", "-m", "merge feature", "feature")
	shas["merge"] = head()

	bare := filepath.Join(reposDir, cOwner, cRepo+".git")
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
	return s, shas
}

func findFile(files []api.DiffFile, path string) *api.DiffFile {
	for i := range files {
		if files[i].NewPath == path || files[i].OldPath == path {
			return &files[i]
		}
	}
	return nil
}

func TestHandleCommitRootCommit(t *testing.T) {
	s, shas := newDiffTestServer(t)
	out, code := getCommitDetail(t, s, shas["root"])
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if len(out.Parents) != 0 {
		t.Errorf("root parents = %v, want none", out.Parents)
	}
	if out.Commit.Subject != "root commit" {
		t.Errorf("subject = %q", out.Commit.Subject)
	}
	a := findFile(out.Files, "a.txt")
	if a == nil {
		t.Fatal("a.txt not in root diff")
	}
	if a.Status != "added" {
		t.Errorf("a.txt status = %q, want added", a.Status)
	}
	if a.Additions != 3 || a.Deletions != 0 {
		t.Errorf("a.txt +%d -%d, want +3 -0", a.Additions, a.Deletions)
	}
	// Line numbering: every line is an add on the new side.
	if len(a.Hunks) == 0 {
		t.Fatal("no hunks for a.txt")
	}
	first := a.Hunks[0].Lines[0]
	if first.Kind != "add" || first.New != 1 || first.Old != 0 || first.Text != "one" {
		t.Errorf("a.txt line[0] = %+v", first)
	}
}

func TestHandleCommitModify(t *testing.T) {
	s, shas := newDiffTestServer(t)
	out, _ := getCommitDetail(t, s, shas["modify"])
	if len(out.Parents) != 1 {
		t.Fatalf("modify parents = %v, want 1", out.Parents)
	}
	if out.Additions != 1 || out.Deletions != 1 {
		t.Errorf("totals +%d -%d, want +1 -1", out.Additions, out.Deletions)
	}
	a := findFile(out.Files, "a.txt")
	if a == nil || a.Status != "modified" {
		t.Fatalf("a.txt = %+v", a)
	}
	// Expect a context "one", a del "two"@old2, an add "TWO"@new2, context "three".
	var del, add *api.DiffLine
	for i := range a.Hunks[0].Lines {
		l := &a.Hunks[0].Lines[i]
		if l.Kind == "del" {
			del = l
		}
		if l.Kind == "add" {
			add = l
		}
	}
	if del == nil || del.Text != "two" || del.Old != 2 || del.New != 0 {
		t.Errorf("del line = %+v", del)
	}
	if add == nil || add.Text != "TWO" || add.New != 2 || add.Old != 0 {
		t.Errorf("add line = %+v", add)
	}
}

func TestHandleCommitDelete(t *testing.T) {
	s, shas := newDiffTestServer(t)
	out, _ := getCommitDetail(t, s, shas["delete"])
	f := findFile(out.Files, "dir/keep.txt")
	if f == nil || f.Status != "deleted" {
		t.Fatalf("keep.txt = %+v", f)
	}
	if f.Deletions != 1 || f.Additions != 0 {
		t.Errorf("keep.txt +%d -%d, want +0 -1", f.Additions, f.Deletions)
	}
}

func TestHandleCommitBinary(t *testing.T) {
	s, shas := newDiffTestServer(t)
	out, _ := getCommitDetail(t, s, shas["binary"])
	f := findFile(out.Files, "logo.bin")
	if f == nil {
		t.Fatal("logo.bin not in diff")
	}
	if !f.Binary {
		t.Errorf("logo.bin binary = false, want true")
	}
	if len(f.Hunks) != 0 {
		t.Errorf("binary file has %d hunks, want 0", len(f.Hunks))
	}
}

func TestHandleCommitRename(t *testing.T) {
	s, shas := newDiffTestServer(t)
	out, _ := getCommitDetail(t, s, shas["rename"])
	if len(out.Files) != 1 {
		t.Fatalf("rename produced %d files, want 1: %+v", len(out.Files), out.Files)
	}
	f := out.Files[0]
	if f.Status != "renamed" {
		t.Errorf("status = %q, want renamed", f.Status)
	}
	if f.OldPath != "a.txt" || f.NewPath != "renamed.txt" {
		t.Errorf("rename paths = %q -> %q", f.OldPath, f.NewPath)
	}
}

func TestHandleCommitMergeFirstParent(t *testing.T) {
	s, shas := newDiffTestServer(t)
	out, _ := getCommitDetail(t, s, shas["merge"])
	if len(out.Parents) != 2 {
		t.Fatalf("merge parents = %v, want 2", out.Parents)
	}
	if out.Parents[0] != shas["mainTip"] {
		t.Errorf("first parent = %s, want main tip %s", out.Parents[0], shas["mainTip"])
	}
	// Diff against the first parent (main) shows only what the feature branch
	// brought in: feature.txt added.
	f := findFile(out.Files, "feature.txt")
	if f == nil || f.Status != "added" {
		t.Fatalf("feature.txt = %+v", f)
	}
}

func TestHandleCommitInvalidSha(t *testing.T) {
	s, _ := newDiffTestServer(t)
	if _, code := getCommitDetail(t, s, "not-a-sha"); code != http.StatusBadRequest {
		t.Errorf("invalid sha status = %d, want 400", code)
	}
	if _, code := getCommitDetail(t, s, "deadbeef"); code != http.StatusNotFound {
		t.Errorf("unknown sha status = %d, want 404", code)
	}
}
