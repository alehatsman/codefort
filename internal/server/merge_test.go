package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alehatsman/moongit/internal/api"
	"github.com/alehatsman/moongit/internal/config"
	"github.com/alehatsman/moongit/internal/storage"
)

// newMergeTestServer builds a bare repo with branches exercising every merge
// path, and returns the server plus the bare repo dir for ref inspection.
//
//	main      root -> edits a.txt          (base)
//	feature   root -> adds f.txt           (clean vs main: different file)
//	conflict  root -> edits a.txt          (conflicts with main's a.txt edit)
//	ahead     main -> adds ahead.txt       (pure fast-forward over main)
func newMergeTestServer(t *testing.T) (*Server, string) {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "m.db"))
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
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = work
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	write := func(rel, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(work, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	git("init", "-q", "-b", "main")
	write("a.txt", "one\n")
	git("add", ".")
	git("commit", "-q", "-m", "root commit")

	git("checkout", "-q", "-b", "feature")
	write("f.txt", "feature\n")
	git("add", ".")
	git("commit", "-q", "-m", "feature work")

	git("checkout", "-q", "-b", "conflict", "main")
	write("a.txt", "conflict side\n")
	git("add", ".")
	git("commit", "-q", "-m", "conflict edit")

	git("checkout", "-q", "main")
	write("a.txt", "main side\n")
	git("add", ".")
	git("commit", "-q", "-m", "main edit")

	git("checkout", "-q", "-b", "ahead", "main")
	write("ahead.txt", "ahead\n")
	git("add", ".")
	git("commit", "-q", "-m", "ahead work")

	git("checkout", "-q", "main")

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
	return s, bare
}

// bareRev resolves a ref in the bare repo to its OID.
func bareRev(t *testing.T, bare, ref string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--verify", ref)
	cmd.Dir = bare
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse %s: %v: %s", ref, err, out)
	}
	return strings.TrimSpace(string(out))
}

// bareParentCount returns the number of parents of the commit at ref.
// `rev-list --parents -n 1` prints "<sha> <parent>..." on one line.
func bareParentCount(t *testing.T, bare, ref string) int {
	t.Helper()
	cmd := exec.Command("git", "rev-list", "--parents", "-n", "1", ref)
	cmd.Dir = bare
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rev-list --parents %s: %v: %s", ref, err, out)
	}
	return len(strings.Fields(string(out))) - 1
}

// openPull inserts an open PR directly via storage.
func openPull(t *testing.T, s *Server, base, head, title string) api.PullRequest {
	t.Helper()
	repoID, err := storage.LookupRepo(s.rdb, cOwner, cRepo)
	if err != nil {
		t.Fatalf("LookupRepo: %v", err)
	}
	pr, err := storage.CreatePull(s.db, repoID, api.CreatePullRequest{
		Base: base, Head: head, Title: title, Author: "alice",
	})
	if err != nil {
		t.Fatalf("CreatePull: %v", err)
	}
	return pr
}

func mergeResult(t *testing.T, rr *http.Response) api.MergeResult {
	t.Helper()
	var res api.MergeResult
	body, _ := io.ReadAll(rr.Body)
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("decode merge result: %v (body=%s)", err, body)
	}
	return res
}

func TestMergeCommitClean(t *testing.T) {
	s, bare := newMergeTestServer(t)
	openPull(t, s, "main", "feature", "add feature")

	rr := drivePull(t, s, s.handleMergePull, http.MethodPost,
		"/api/repos/alice/proj/pulls/1/merge", "agent#7", "1", api.MergeRequest{Method: api.MergeCommitMethod})
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", rr.Code, rr.Body.String())
	}
	res := mergeResult(t, rr.Result())
	if res.FastForward {
		t.Error("diverged merge reported as fast-forward")
	}
	if res.State != api.PRMerged || res.MergedAt == nil {
		t.Errorf("PR state=%q merged_at=%v, want merged/non-nil", res.State, res.MergedAt)
	}
	// base now points at the merge commit, which has two parents.
	if got := bareRev(t, bare, "refs/heads/main"); got != res.MergeCommit {
		t.Errorf("main = %s, want merge commit %s", got, res.MergeCommit)
	}
	if n := bareParentCount(t, bare, "refs/heads/main"); n != 2 {
		t.Errorf("merge commit has %d parents, want 2", n)
	}
	// feature.txt is now reachable from main.
	cmd := exec.Command("git", "cat-file", "-e", "refs/heads/main:f.txt")
	cmd.Dir = bare
	if err := cmd.Run(); err != nil {
		t.Errorf("f.txt not reachable from main after merge: %v", err)
	}
}

// TestMergedPullDetailShowsDiff guards the regression where a merged PR's
// detail rendered an empty compare: once head is merged into base, the live
// base..head diff is empty. The merge freezes the pre-merge tips so the detail
// endpoint still reproduces the PR's diff.
func TestMergedPullDetailShowsDiff(t *testing.T) {
	s, _ := newMergeTestServer(t)
	openPull(t, s, "main", "feature", "add feature")

	// Sanity: while open, the compare shows feature.txt one commit ahead.
	rr := drivePull(t, s, s.handleGetPull, http.MethodGet, "/api/repos/alice/proj/pulls/1", "agent#7", "1", nil)
	var before api.PullRequestDetail
	if err := json.Unmarshal(rr.Body.Bytes(), &before); err != nil {
		t.Fatalf("decode open detail: %v", err)
	}
	if before.Compare.Ahead != 1 || len(before.Compare.Files) != 1 {
		t.Fatalf("open compare ahead=%d files=%d, want 1/1", before.Compare.Ahead, len(before.Compare.Files))
	}

	// Merge it (diverged → merge commit).
	rr = drivePull(t, s, s.handleMergePull, http.MethodPost,
		"/api/repos/alice/proj/pulls/1/merge", "agent#7", "1", api.MergeRequest{Method: api.MergeCommitMethod})
	if rr.Code != http.StatusOK {
		t.Fatalf("merge status = %d (body=%s)", rr.Code, rr.Body.String())
	}

	// After merge the detail must still carry the diff (from the frozen tips),
	// not the empty live-ref compare.
	rr = drivePull(t, s, s.handleGetPull, http.MethodGet, "/api/repos/alice/proj/pulls/1", "agent#7", "1", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("get merged status = %d (body=%s)", rr.Code, rr.Body.String())
	}
	var after api.PullRequestDetail
	if err := json.Unmarshal(rr.Body.Bytes(), &after); err != nil {
		t.Fatalf("decode merged detail: %v", err)
	}
	if after.State != api.PRMerged {
		t.Fatalf("state = %q, want merged", after.State)
	}
	if after.Compare.Ahead != 1 {
		t.Errorf("merged compare ahead = %d, want 1 (frozen pre-merge)", after.Compare.Ahead)
	}
	if len(after.Compare.Files) != 1 || after.Compare.Files[0].NewPath != "f.txt" {
		t.Errorf("merged compare files = %+v, want [f.txt]", after.Compare.Files)
	}
	// Branch short names are preserved for display even though the diff was
	// computed from SHAs.
	if after.Compare.Base != "main" || after.Compare.Head != "feature" {
		t.Errorf("compare refs = %q/%q, want main/feature", after.Compare.Base, after.Compare.Head)
	}
}

func TestMergeConflict(t *testing.T) {
	s, bare := newMergeTestServer(t)
	openPull(t, s, "main", "conflict", "conflicting change")
	before := bareRev(t, bare, "refs/heads/main")

	rr := drivePull(t, s, s.handleMergePull, http.MethodPost,
		"/api/repos/alice/proj/pulls/1/merge", "agent#7", "1", api.MergeRequest{Method: api.MergeCommitMethod})
	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body=%s)", rr.Code, rr.Body.String())
	}
	var resp api.MergeConflictResponse
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if len(resp.Conflicts) != 1 || resp.Conflicts[0] != "a.txt" {
		t.Errorf("conflicts = %v, want [a.txt]", resp.Conflicts)
	}
	// base ref unchanged, PR still open.
	if after := bareRev(t, bare, "refs/heads/main"); after != before {
		t.Errorf("main moved on conflict: %s -> %s", before, after)
	}
	repoID, _ := storage.LookupRepo(s.rdb, cOwner, cRepo)
	pr, _ := storage.GetPull(s.rdb, repoID, 1)
	if pr.State != api.PROpen {
		t.Errorf("PR state = %q after conflict, want open", pr.State)
	}
}

func TestMergeFastForward(t *testing.T) {
	s, bare := newMergeTestServer(t)
	openPull(t, s, "main", "ahead", "ff me")
	headTip := bareRev(t, bare, "refs/heads/ahead")

	rr := drivePull(t, s, s.handleMergePull, http.MethodPost,
		"/api/repos/alice/proj/pulls/1/merge", "agent#7", "1", api.MergeRequest{Method: api.MergeFFOnlyMethod})
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", rr.Code, rr.Body.String())
	}
	res := mergeResult(t, rr.Result())
	if !res.FastForward {
		t.Error("fast-forward not reported")
	}
	// FF moves base straight to head — no merge commit.
	if got := bareRev(t, bare, "refs/heads/main"); got != headTip {
		t.Errorf("main = %s, want head tip %s (fast-forward)", got, headTip)
	}
	if res.MergeCommit != headTip {
		t.Errorf("merge_commit = %s, want head tip %s", res.MergeCommit, headTip)
	}
}

func TestMergeFFOnlyRejectsDiverged(t *testing.T) {
	s, _ := newMergeTestServer(t)
	openPull(t, s, "main", "feature", "diverged")

	rr := drivePull(t, s, s.handleMergePull, http.MethodPost,
		"/api/repos/alice/proj/pulls/1/merge", "agent#7", "1", api.MergeRequest{Method: api.MergeFFOnlyMethod})
	if rr.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 (not fast-forwardable)", rr.Code)
	}
}

func TestMergeDefaultMethodIsMergeCommit(t *testing.T) {
	s, bare := newMergeTestServer(t)
	openPull(t, s, "main", "feature", "default method")

	// Empty body → default merge commit, even though nothing forces it.
	rr := drivePull(t, s, s.handleMergePull, http.MethodPost,
		"/api/repos/alice/proj/pulls/1/merge", "agent#7", "1", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", rr.Code, rr.Body.String())
	}
	res := mergeResult(t, rr.Result())
	if res.FastForward {
		t.Error("default method fast-forwarded a diverged merge")
	}
	if n := bareParentCount(t, bare, "refs/heads/main"); n != 2 {
		t.Errorf("default merge has %d parents, want 2", n)
	}
}

func TestMergeAlreadyMerged(t *testing.T) {
	s, _ := newMergeTestServer(t)
	pr := openPull(t, s, "main", "feature", "twice")
	repoID, _ := storage.LookupRepo(s.rdb, cOwner, cRepo)
	merged := api.PRMerged
	storage.UpdatePull(s.db, repoID, pr.Number, nil, nil, &merged)

	rr := drivePull(t, s, s.handleMergePull, http.MethodPost,
		"/api/repos/alice/proj/pulls/1/merge", "agent#7", "1", nil)
	if rr.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 (already merged)", rr.Code)
	}
}

func TestMergeNothingToMerge(t *testing.T) {
	s, _ := newMergeTestServer(t)
	// head=main into base=ahead: ahead already contains main, so there is
	// nothing to bring in.
	openPull(t, s, "ahead", "main", "nothing")
	rr := drivePull(t, s, s.handleMergePull, http.MethodPost,
		"/api/repos/alice/proj/pulls/1/merge", "agent#7", "1", nil)
	if rr.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 (nothing to merge)", rr.Code)
	}
}

func TestMergeInvalidMethod(t *testing.T) {
	s, _ := newMergeTestServer(t)
	openPull(t, s, "main", "feature", "bad method")
	rr := drivePull(t, s, s.handleMergePull, http.MethodPost,
		"/api/repos/alice/proj/pulls/1/merge", "agent#7", "1", api.MergeRequest{Method: "squash"})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (invalid method)", rr.Code)
	}
}

// TestUpdateRefCAS pins the compare-and-swap guard that protects the merge ref
// update from a concurrent receive-pack: update-ref must fail when the supplied
// old OID no longer matches the ref's current value.
func TestUpdateRefCAS(t *testing.T) {
	_, bare := newMergeTestServer(t)
	ctx := context.Background()
	mainTip := bareRev(t, bare, "refs/heads/main")
	featureTip := bareRev(t, bare, "refs/heads/feature")

	// Wrong old value (featureTip) → rejected, ref unchanged.
	if err := updateRef(ctx, bare, "refs/heads/main", featureTip, featureTip); err == nil {
		t.Error("update-ref with stale old OID succeeded, want CAS failure")
	}
	if got := bareRev(t, bare, "refs/heads/main"); got != mainTip {
		t.Errorf("main moved despite failed CAS: %s", got)
	}
	// Correct old value → accepted.
	if err := updateRef(ctx, bare, "refs/heads/main", featureTip, mainTip); err != nil {
		t.Errorf("update-ref with correct old OID failed: %v", err)
	}
	if got := bareRev(t, bare, "refs/heads/main"); got != featureTip {
		t.Errorf("main = %s, want %s after valid CAS", got, featureTip)
	}
}
