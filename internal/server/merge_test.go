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

func TestMergeHeadAlreadyInBase(t *testing.T) {
	s, _ := newMergeTestServer(t)
	// head=main into base=ahead: ahead already contains main (head is an
	// ancestor of base). The endpoint now marks the PR merged and returns 200
	// instead of 409 so that callers that merged via direct push don't hit an
	// error when they also invoke the PR merge endpoint.
	openPull(t, s, "ahead", "main", "already-in-base")
	rr := drivePull(t, s, s.handleMergePull, http.MethodPost,
		"/api/repos/alice/proj/pulls/1/merge", "agent#7", "1", nil)
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (head already in base → mark merged)", rr.Code)
	}
	res := mergeResult(t, rr.Result())
	if res.PullRequest.State != api.PRMerged {
		t.Errorf("pr state = %s, want merged", res.PullRequest.State)
	}
	if !res.FastForward {
		t.Error("want FastForward=true for head-already-in-base path")
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

// A server-side merge moves the base ref with update-ref, not receive-pack, so
// the post-receive hook never fires. Without an explicit enqueue the canonical
// branch would accept merges and never build them (#256).
func TestMergeEnqueuesCIRunOnBase(t *testing.T) {
	s, _ := newMergeTestServer(t)
	repoID, err := storage.LookupRepo(s.rdb, cOwner, cRepo)
	if err != nil {
		t.Fatalf("LookupRepo: %v", err)
	}
	if err := storage.SetRepoCIEnabled(s.db, repoID, true); err != nil {
		t.Fatalf("SetRepoCIEnabled: %v", err)
	}
	openPull(t, s, "main", "feature", "add feature")

	rr := drivePull(t, s, s.handleMergePull, http.MethodPost,
		"/api/repos/alice/proj/pulls/1/merge", "agent#7", "1", api.MergeRequest{Method: api.MergeCommitMethod})
	if rr.Code != http.StatusOK {
		t.Fatalf("merge status = %d (body=%s)", rr.Code, rr.Body.String())
	}
	res := mergeResult(t, rr.Result())

	runs, err := storage.ListRuns(s.rdb, repoID, storage.RunFilter{})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want exactly one queued by the merge", len(runs))
	}
	got := runs[0]
	if got.Ref != "refs/heads/main" {
		t.Errorf("run ref = %q, want refs/heads/main (the base branch, not the head)", got.Ref)
	}
	// The run must build the *merge result*, not either pre-merge tip —
	// building the head commit would test code that was never on main.
	if got.CommitSHA != res.MergeCommit {
		t.Errorf("run commit = %s, want the merge commit %s", got.CommitSHA, res.MergeCommit)
	}
	if got.Event != "merge" {
		t.Errorf("run event = %q, want %q so the UI can tell it from a push", got.Event, "merge")
	}
	if got.Trigger != "agent#7" {
		t.Errorf("run trigger = %q, want the merging identity", got.Trigger)
	}
}

// CI disabled is an ordinary outcome, not a merge failure.
func TestMergeWithCIDisabledStillMerges(t *testing.T) {
	s, bare := newMergeTestServer(t)
	repoID, err := storage.LookupRepo(s.rdb, cOwner, cRepo)
	if err != nil {
		t.Fatalf("LookupRepo: %v", err)
	}
	openPull(t, s, "main", "feature", "add feature")

	rr := drivePull(t, s, s.handleMergePull, http.MethodPost,
		"/api/repos/alice/proj/pulls/1/merge", "agent#7", "1", api.MergeRequest{Method: api.MergeCommitMethod})
	if rr.Code != http.StatusOK {
		t.Fatalf("merge status = %d (body=%s)", rr.Code, rr.Body.String())
	}
	res := mergeResult(t, rr.Result())
	if got := bareRev(t, bare, "refs/heads/main"); got != res.MergeCommit {
		t.Errorf("main = %s, want the merge commit %s", got, res.MergeCommit)
	}
	runs, err := storage.ListRuns(s.rdb, repoID, storage.RunFilter{})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 0 {
		t.Errorf("runs = %d, want none when CI is off for the repo", len(runs))
	}
}

// --- Rebase method (#257) --------------------------------------------------

// The fixture's `feature` branch forked before main's edit, so it is diverged:
// ff-only rejects it and a merge commit would fork the history. Rebase replays
// feature's one commit onto main's tip, producing a linear result.
func TestMergeRebaseReplaysOntoBase(t *testing.T) {
	s, bare := newMergeTestServer(t)
	openPull(t, s, "main", "feature", "rebase me")
	baseTip := bareRev(t, bare, "refs/heads/main")
	headTip := bareRev(t, bare, "refs/heads/feature")

	rr := drivePull(t, s, s.handleMergePull, http.MethodPost,
		"/api/repos/alice/proj/pulls/1/merge", "agent#7", "1", api.MergeRequest{Method: api.MergeRebaseMethod})
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", rr.Code, rr.Body.String())
	}
	res := mergeResult(t, rr.Result())

	newTip := bareRev(t, bare, "refs/heads/main")
	if newTip != res.MergeCommit {
		t.Errorf("main = %s, merge_commit = %s; want the same commit", newTip, res.MergeCommit)
	}
	// Linear, not a merge: exactly one parent, and it is the old base tip.
	if n := bareParentCount(t, bare, "refs/heads/main"); n != 1 {
		t.Errorf("new tip has %d parents, want 1 (rebase must not create a merge commit)", n)
	}
	if p := bareRev(t, bare, "refs/heads/main^"); p != baseTip {
		t.Errorf("new tip's parent = %s, want the pre-merge base tip %s", p, baseTip)
	}
	// It is a replay, not a move: a new commit object carrying the same content.
	if newTip == headTip {
		t.Error("main points at the original head commit; want a replayed commit")
	}
	if !res.FastForward {
		t.Error("a rebase lands linearly on base; want fast_forward true")
	}

	// Both sides' content is present — main's edit survived and feature's file
	// arrived, which is the whole point of replaying rather than resetting.
	if got := bareFile(t, bare, "refs/heads/main", "a.txt"); got != "main side\n" {
		t.Errorf("a.txt = %q, want main's edit preserved", got)
	}
	if got := bareFile(t, bare, "refs/heads/main", "f.txt"); got != "feature\n" {
		t.Errorf("f.txt = %q, want feature's file replayed", got)
	}

	// The head ref is deliberately untouched: the replayed commits are new
	// objects, and rewriting a published branch is not the server's call.
	if got := bareRev(t, bare, "refs/heads/feature"); got != headTip {
		t.Errorf("feature moved to %s; rebase must not rewrite the head ref", got)
	}
}

// Authorship survives a replay; only the committer becomes the server.
func TestMergeRebasePreservesAuthor(t *testing.T) {
	s, bare := newMergeTestServer(t)
	openPull(t, s, "main", "feature", "keep my name")

	rr := drivePull(t, s, s.handleMergePull, http.MethodPost,
		"/api/repos/alice/proj/pulls/1/merge", "agent#7", "1", api.MergeRequest{Method: api.MergeRebaseMethod})
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", rr.Code, rr.Body.String())
	}
	if got := bareShow(t, bare, "refs/heads/main", "%an"); got != "Alice" {
		t.Errorf("author = %q, want the original author Alice", got)
	}
	if got := bareShow(t, bare, "refs/heads/main", "%cn"); got != "moongit" {
		t.Errorf("committer = %q, want moongit (the server did the replay)", got)
	}
	if got := bareShow(t, bare, "refs/heads/main", "%s"); got != "feature work" {
		t.Errorf("subject = %q, want the original message preserved", got)
	}
}

// A conflicting replay reports the same 409 + path list a merge does, and
// leaves the base ref where it was — a half-applied rebase is the failure mode
// worth ruling out, since each commit lands as its own update.
func TestMergeRebaseConflictLeavesBaseUntouched(t *testing.T) {
	s, bare := newMergeTestServer(t)
	openPull(t, s, "main", "conflict", "conflicting rebase")
	before := bareRev(t, bare, "refs/heads/main")

	rr := drivePull(t, s, s.handleMergePull, http.MethodPost,
		"/api/repos/alice/proj/pulls/1/merge", "agent#7", "1", api.MergeRequest{Method: api.MergeRebaseMethod})
	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body=%s)", rr.Code, rr.Body.String())
	}
	var resp api.MergeConflictResponse
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if len(resp.Conflicts) != 1 || resp.Conflicts[0] != "a.txt" {
		t.Errorf("conflicts = %v, want [a.txt]", resp.Conflicts)
	}
	if after := bareRev(t, bare, "refs/heads/main"); after != before {
		t.Errorf("main moved on a conflicting rebase: %s -> %s", before, after)
	}
	repoID, _ := storage.LookupRepo(s.rdb, cOwner, cRepo)
	pr, _ := storage.GetPull(s.rdb, repoID, 1)
	if pr.State != api.PROpen {
		t.Errorf("PR state = %q after conflict, want open", pr.State)
	}
}

// An already-linear head needs no replay: rebase fast-forwards instead of
// minting new SHAs for commits that are already on top of base.
func TestMergeRebaseFastForwardsWhenAlreadyLinear(t *testing.T) {
	s, bare := newMergeTestServer(t)
	openPull(t, s, "main", "ahead", "already linear")
	headTip := bareRev(t, bare, "refs/heads/ahead")

	rr := drivePull(t, s, s.handleMergePull, http.MethodPost,
		"/api/repos/alice/proj/pulls/1/merge", "agent#7", "1", api.MergeRequest{Method: api.MergeRebaseMethod})
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", rr.Code, rr.Body.String())
	}
	if got := bareRev(t, bare, "refs/heads/main"); got != headTip {
		t.Errorf("main = %s, want the existing head tip %s (no replay needed)", got, headTip)
	}
}

// bareFile returns a file's content at a ref in the bare repo.
func bareFile(t *testing.T, bare, ref, path string) string {
	t.Helper()
	cmd := exec.Command("git", "show", ref+":"+path)
	cmd.Dir = bare
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git show %s:%s: %v: %s", ref, path, err, out)
	}
	return string(out)
}

// bareShow returns one `git show -s --format=<f>` field for a ref.
func bareShow(t *testing.T, bare, ref, format string) string {
	t.Helper()
	cmd := exec.Command("git", "show", "-s", "--format="+format, ref)
	cmd.Dir = bare
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git show -s %s %s: %v: %s", format, ref, err, out)
	}
	return strings.TrimSpace(string(out))
}

// --- Review gate on merge ---------------------------------------------------

// setRequireApproval flips the repo's opt-in review gate.
func setRequireApproval(t *testing.T, s *Server, on bool) {
	t.Helper()
	repoID, err := storage.LookupRepo(s.rdb, cOwner, cRepo)
	if err != nil {
		t.Fatalf("LookupRepo: %v", err)
	}
	if err := storage.SetRepoRequireApproval(s.db, repoID, on); err != nil {
		t.Fatalf("SetRepoRequireApproval: %v", err)
	}
}

func review(t *testing.T, s *Server, num int, author string, state api.PRReviewState) {
	t.Helper()
	repoID, _ := storage.LookupRepo(s.rdb, cOwner, cRepo)
	if _, err := storage.UpsertReview(s.db, repoID, num, author, state); err != nil {
		t.Fatalf("UpsertReview: %v", err)
	}
}

// Default is off: an unreviewed PR merges exactly as it did before the gate
// existed. This is the upgrade-safety property — the rest of the suite merges
// without ever recording a review.
func TestMergeGateOffByDefaultAllowsUnreviewedMerge(t *testing.T) {
	s, bare := newMergeTestServer(t)
	openPull(t, s, "main", "feature", "no review")
	before := bareRev(t, bare, "refs/heads/main")

	rr := drivePull(t, s, s.handleMergePull, http.MethodPost,
		"/api/repos/alice/proj/pulls/1/merge", "agent#7", "1", api.MergeRequest{})
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with the gate off (body=%s)", rr.Code, rr.Body.String())
	}
	if after := bareRev(t, bare, "refs/heads/main"); after == before {
		t.Error("main did not move")
	}
}

func TestMergeGateRejectsUnapprovedPull(t *testing.T) {
	s, bare := newMergeTestServer(t)
	openPull(t, s, "main", "feature", "needs a review")
	setRequireApproval(t, s, true)
	before := bareRev(t, bare, "refs/heads/main")

	rr := drivePull(t, s, s.handleMergePull, http.MethodPost,
		"/api/repos/alice/proj/pulls/1/merge", "agent#7", "1", api.MergeRequest{})
	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body=%s)", rr.Code, rr.Body.String())
	}
	// The gate runs before anything touches a ref.
	if after := bareRev(t, bare, "refs/heads/main"); after != before {
		t.Errorf("main moved despite the gate: %s -> %s", before, after)
	}
	repoID, _ := storage.LookupRepo(s.rdb, cOwner, cRepo)
	pr, _ := storage.GetPull(s.rdb, repoID, 1)
	if pr.State != api.PROpen {
		t.Errorf("PR state = %q, want it left open", pr.State)
	}
}

func TestMergeGateAllowsApprovedPull(t *testing.T) {
	s, bare := newMergeTestServer(t)
	openPull(t, s, "main", "feature", "approved")
	setRequireApproval(t, s, true)
	review(t, s, 1, "bob", api.PRReviewApproved)
	before := bareRev(t, bare, "refs/heads/main")

	rr := drivePull(t, s, s.handleMergePull, http.MethodPost,
		"/api/repos/alice/proj/pulls/1/merge", "agent#7", "1", api.MergeRequest{})
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
	if after := bareRev(t, bare, "refs/heads/main"); after == before {
		t.Error("main did not move on an approved merge")
	}
}

// An outstanding changes_requested blocks even when someone else approved —
// the objection is the stronger signal, and it names who raised it.
func TestMergeGateChangesRequestedBeatsAnApproval(t *testing.T) {
	s, _ := newMergeTestServer(t)
	openPull(t, s, "main", "feature", "contested")
	setRequireApproval(t, s, true)
	review(t, s, 1, "bob", api.PRReviewApproved)
	review(t, s, 1, "carol", api.PRReviewChangesRequested)

	rr := drivePull(t, s, s.handleMergePull, http.MethodPost,
		"/api/repos/alice/proj/pulls/1/merge", "agent#7", "1", api.MergeRequest{})
	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body=%s)", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "carol") {
		t.Errorf("409 should name who requested changes; got %s", rr.Body.String())
	}
}

// A verdict replaces the reviewer's previous one, so withdrawing an objection
// by approving unblocks the merge without a second reviewer.
func TestMergeGateReviewerCanWithdrawObjection(t *testing.T) {
	s, _ := newMergeTestServer(t)
	openPull(t, s, "main", "feature", "reconsidered")
	setRequireApproval(t, s, true)
	review(t, s, 1, "bob", api.PRReviewChangesRequested)
	review(t, s, 1, "bob", api.PRReviewApproved)

	rr := drivePull(t, s, s.handleMergePull, http.MethodPost,
		"/api/repos/alice/proj/pulls/1/merge", "agent#7", "1", api.MergeRequest{})
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 after the objection was withdrawn (body=%s)", rr.Code, rr.Body.String())
	}
}
