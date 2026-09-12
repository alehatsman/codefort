package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/alehatsman/moongit/internal/api"
	"github.com/alehatsman/moongit/internal/storage"
)

// handleMergePull merges a PR's head branch into its base branch inside the
// server's bare repo, then marks the PR merged. The merge runs without a
// worktree (merge-tree --write-tree + commit-tree + update-ref), so it is safe
// against the same bare repo smart-HTTP serves. Conflicts are reported, never
// auto-resolved: the author rebases locally and pushes.
func (s *Server) handleMergePull(w http.ResponseWriter, r *http.Request) {
	num, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || num <= 0 {
		writeError(w, http.StatusBadRequest, "invalid pull request number")
		return
	}

	var req api.MergeRequest
	// An empty body is allowed (defaults to a merge commit).
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
	}
	if req.Method == "" {
		req.Method = api.MergeCommitMethod
	}
	if !req.Method.Valid() {
		writeError(w, http.StatusBadRequest, "invalid method: "+string(req.Method))
		return
	}

	repoID, ok := s.lookupRepoOrFail(w, r)
	if !ok {
		return
	}
	repoDir, ok := s.repoDirOrFail(w, r)
	if !ok {
		return
	}

	pr, err := storage.GetPull(s.rdb, repoID, num)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "pull request not found")
		return
	}
	if err != nil {
		s.logger.Error("get pull for merge", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if pr.State != api.PROpen {
		writeError(w, http.StatusConflict, "pull request is "+string(pr.State)+", not open")
		return
	}

	if msg, ok := s.reviewGateBlocks(repoID, num); ok {
		writeError(w, http.StatusConflict, msg)
		return
	}

	// Both branches must still exist (a branch can be deleted after the PR is
	// opened). The names came from refs/heads at create time but are re-checked
	// here since the diff and ref updates interpolate them.
	if !branchExists(r.Context(), repoDir, pr.BaseRef) {
		writeError(w, http.StatusConflict, "base branch no longer exists: "+pr.BaseRef)
		return
	}
	if !branchExists(r.Context(), repoDir, pr.HeadRef) {
		writeError(w, http.StatusConflict, "head branch no longer exists: "+pr.HeadRef)
		return
	}

	baseTip, err := revParse(r.Context(), repoDir, "refs/heads/"+pr.BaseRef)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "resolve base failed")
		return
	}
	headTip, err := revParse(r.Context(), repoDir, "refs/heads/"+pr.HeadRef)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "resolve head failed")
		return
	}

	// Head is already contained in base: the head was merged via a direct push
	// (bypassing this endpoint). Mark the PR merged and return success so the
	// caller doesn't have to treat this as an error. We use headTip as the
	// frozen pre-merge base approximation; the pre-push base is unknowable here,
	// so the frozen diff will be empty — same caveat as autoCloseMergedPRs.
	if isAncestor(r.Context(), repoDir, headTip, baseTip) {
		updated, err := storage.MarkMerged(s.db, repoID, num, headTip, headTip)
		if err != nil {
			s.logger.Error("mark pull merged (head already in base)", "repo", repoDir, "pr", num, "err", err)
			writeError(w, http.StatusInternalServerError, "failed to update PR state")
			return
		}
		// Head reached base via a direct push that may not have hit the mirror;
		// push the current base out so an external mirror stays in lockstep.
		s.mirrorMergedBranch(repoDir, pr.BaseRef)
		s.closeLinkedIssues(repoID, pr)
		s.emitPull("pull.merged", repoID, identityFromContext(r), updated)
		writeJSON(w, http.StatusOK, api.MergeResult{
			PullRequest: updated,
			MergeCommit: baseTip,
			FastForward: true,
		})
		return
	}

	newTip, ff, conflicts, err := s.doMerge(r.Context(), repoDir, pr, req.Method, baseTip, headTip, identityFromContext(r))
	switch {
	case errors.Is(err, errNothingToRebase):
		writeError(w, http.StatusConflict, "nothing to rebase; the head branch adds only merge commits over base")
		return
	case errors.Is(err, errNotFastForward):
		writeError(w, http.StatusConflict, "not fast-forwardable; use method \"merge\" or \"rebase\"")
		return
	case errors.Is(err, errBaseMoved):
		writeError(w, http.StatusConflict, "base branch moved during merge; retry")
		return
	case len(conflicts) > 0:
		writeJSON(w, http.StatusConflict, api.MergeConflictResponse{
			Error:     "merge conflict; resolve locally and push",
			Conflicts: conflicts,
		})
		return
	case err != nil:
		s.logger.Error("merge pull", "repo", repoDir, "pr", num, "err", err)
		writeError(w, http.StatusInternalServerError, "merge failed")
		return
	}

	// Freeze the pre-merge base/head tips so the detail endpoint can reproduce
	// the diff: post-merge head is contained in base, so a live-ref compare goes
	// empty. baseTip/headTip are the tips resolved above, before the ref moved.
	updated, err := storage.MarkMerged(s.db, repoID, num, baseTip, headTip)
	if err != nil {
		// The refs are already merged; report it but log the bookkeeping miss.
		s.logger.Error("mark pull merged", "repo", repoDir, "pr", num, "err", err)
		writeError(w, http.StatusInternalServerError, "merged refs but failed to update PR state")
		return
	}
	// The merge moved the base ref directly in the bare repo (no git push), so
	// push it out to the mirror remote, if configured, to keep an external
	// mirror in sync. Best-effort and async — the merge already succeeded.
	s.mirrorMergedBranch(repoDir, pr.BaseRef)
	s.closeLinkedIssues(repoID, pr)
	s.enqueueMergeRun(r, repoID, pr.BaseRef, newTip)
	s.emitPull("pull.merged", repoID, identityFromContext(r), updated)
	writeJSON(w, http.StatusOK, api.MergeResult{
		PullRequest: updated,
		MergeCommit: newTip,
		FastForward: ff,
	})
}

// reviewGateBlocks reports whether the repo's opt-in review gate refuses this
// merge, and why. It runs before any ref is touched.
//
// The gate is off by default: review verdicts stay advisory unless a repo turns
// it on, so an upgrade never starts rejecting merges a fleet was already making.
// When it is on the rule is deliberately coarse — at least one standing
// approval, and no outstanding changes_requested — because the alternative is a
// policy engine (reviewer counts, required reviewers, per-branch rules), which
// VISION.md rules out. Verdicts are one-per-reviewer and replace on re-submit,
// so "standing" is just the current row.
//
// A failure to read the gate blocks the merge rather than waving it through:
// the only reason to enable it is that merging unreviewed is not acceptable
// here, so an unreadable setting must not silently mean "no gate".
func (s *Server) reviewGateBlocks(repoID int64, num int) (string, bool) {
	required, err := storage.RepoRequiresApproval(s.rdb, repoID)
	if err != nil {
		s.logger.Error("read review gate", "repo_id", repoID, "pr", num, "err", err)
		return "cannot verify the repo's review requirement; merge refused", true
	}
	if !required {
		return "", false
	}
	reviews, err := storage.ListReviews(s.rdb, repoID, num)
	if err != nil {
		s.logger.Error("list reviews for merge gate", "repo_id", repoID, "pr", num, "err", err)
		return "cannot verify this pull request's reviews; merge refused", true
	}
	approvals := 0
	for _, rev := range reviews {
		switch rev.State {
		case api.PRReviewChangesRequested:
			return "changes requested by " + rev.Author + "; resolve it before merging", true
		case api.PRReviewApproved:
			approvals++
		}
	}
	if approvals == 0 {
		return "this repository requires an approving review before merge", true
	}
	return "", false
}

// enqueueMergeRun builds the base branch at its new tip after a server-side
// merge. The merge moves the ref with update-ref rather than receive-pack, so
// the post-receive hook never fires — without this the canonical branch would
// accept merges and never build them, which is the one branch where a red
// build matters most.
//
// Best-effort, like the mirror push and the linked-issue close above: the
// merge already succeeded and its refs are already moved, so a bookkeeping
// failure is logged, never returned. CI being disabled or the branch being
// filtered out is a normal outcome, not an error.
func (s *Server) enqueueMergeRun(r *http.Request, repoID int64, baseRef, newTip string) {
	owner := r.PathValue("owner")
	name := strings.TrimSuffix(r.PathValue("repo"), ".git")
	if _, _, err := s.enqueueRefRun(
		repoID, owner, name, "refs/heads/"+baseRef, newTip, "merge", identityFromContext(r),
	); err != nil {
		s.logger.Error("merge: enqueue ci run", "repo", owner+"/"+name, "ref", baseRef, "err", err)
	}
}

// mirrorMergedBranch best-effort pushes branch to the repo's "mirror" remote,
// if one is configured. Server-side merges move refs with update-ref rather than
// receive-pack, so without this an external mirror (e.g. GitHub) silently drifts
// from the canonical server. Fire-and-forget with its own timeout: the server is
// the source of truth and the mirror is eventual, so a mirror failure must never
// fail or delay the merge. A no-op when the repo has no "mirror" remote, leaving
// repos that haven't opted in untouched.
//
// The push is plain (never --force): if the mirror diverged (someone pushed
// straight to it), the non-fast-forward push is rejected and logged rather than
// clobbering external commits — reconcile by hand, the same fix a divergence
// always needs.
func (s *Server) mirrorMergedBranch(repoDir, branch string) {
	if !hasMirrorRemote(repoDir) {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := pushMirror(ctx, repoDir, branch); err != nil {
			s.logger.Warn("mirror push failed", "repo", repoDir, "branch", branch, "err", err)
			return
		}
		s.logger.Info("mirrored merged branch", "repo", repoDir, "branch", branch)
	}()
}

// hasMirrorRemote reports whether the bare repo has a remote named "mirror" —
// the opt-in signal for mirroring. Config lives in the repo's git config (set
// with `git -C <repo> remote add mirror <url>`), so there's no moongit-side
// schema or secret to manage. Any error (git missing, not a repo) reads as "no
// mirror", so mirroring stays silent rather than noisy on a misconfigured repo.
func hasMirrorRemote(repoDir string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := gitOutput(ctx, repoDir, "remote")
	if err != nil {
		return false
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		if strings.TrimSpace(line) == "mirror" {
			return true
		}
	}
	return false
}

// pushMirror pushes branch to the "mirror" remote, base→base. Separated from the
// async wrapper so it's directly testable against real bare repos.
func pushMirror(ctx context.Context, repoDir, branch string) error {
	refspec := "refs/heads/" + branch + ":refs/heads/" + branch
	_, err := gitOutput(ctx, repoDir, "push", "mirror", refspec)
	return err
}

// errNotFastForward / errBaseMoved are sentinels doMerge returns so the handler
// can map them to specific 409 messages.
var (
	errNotFastForward = errors.New("not fast-forwardable")
	// errNothingToRebase is its own sentinel so the 409 can say what actually
	// happened: "not fast-forwardable, use rebase" would be nonsense advice to a
	// caller who already asked for rebase.
	errNothingToRebase = errors.New("nothing to rebase")
	errBaseMoved       = errors.New("base ref moved")
)

// doMerge performs the ref update for one of the two methods. On a content
// conflict it returns the conflicting paths (nil error). The old-value guard on
// update-ref (baseTip) makes the ref move atomic against a concurrent push.
func (s *Server) doMerge(ctx context.Context, repoDir string, pr api.PullRequest, method api.MergeMethod, baseTip, headTip, identity string) (newTip string, ff bool, conflicts []string, err error) {
	baseAncestorOfHead := isAncestor(ctx, repoDir, baseTip, headTip)

	if method == api.MergeFFOnlyMethod {
		if !baseAncestorOfHead {
			return "", false, nil, errNotFastForward
		}
		if err := updateRef(ctx, repoDir, "refs/heads/"+pr.BaseRef, headTip, baseTip); err != nil {
			// Almost always the CAS guard failing (a concurrent push moved base),
			// which we surface as a retryable 409. Log the real git error too, so
			// a persistent non-CAS failure (git missing, corrupt refs, perms)
			// doesn't masquerade as a transient conflict with no diagnostic.
			s.logger.Warn("merge update-ref (ff)", "repo", repoDir, "ref", pr.BaseRef, "err", err)
			return "", false, nil, errBaseMoved
		}
		// Verify the ref actually advanced. If update-ref reported success but the
		// ref still points at baseTip (stale lock, transient fs issue, etc.) fail
		// here rather than marking the PR merged with the ref stuck at the old SHA.
		if actual, verifyErr := revParse(ctx, repoDir, "refs/heads/"+pr.BaseRef); verifyErr != nil || actual != headTip {
			s.logger.Error("merge update-ref verify: ref did not advance",
				"repo", repoDir, "ref", pr.BaseRef, "want", headTip, "got", actual, "err", verifyErr)
			return "", false, nil, errBaseMoved
		}
		return headTip, true, nil, nil
	}

	if method == api.MergeRebaseMethod {
		if baseAncestorOfHead {
			// Head already sits on top of base, so replaying would mint new SHAs
			// for commits that are already linear. `git rebase` fast-forwards
			// here for the same reason; so do we.
			return s.doMerge(ctx, repoDir, pr, api.MergeFFOnlyMethod, baseTip, headTip, identity)
		}
		return rebaseOnto(ctx, repoDir, pr, baseTip, headTip)
	}

	// method == merge: build a merge commit without a worktree.
	tree, conflicts, err := mergeTree(ctx, repoDir, baseTip, headTip)
	if err != nil {
		return "", false, nil, err
	}
	if len(conflicts) > 0 {
		return "", false, conflicts, nil
	}

	msg := fmt.Sprintf("Merge pull request #%d from %s into %s\n\n%s", pr.Number, pr.HeadRef, pr.BaseRef, pr.Title)
	commit, err := commitTree(ctx, repoDir, tree, msg, identity, []string{baseTip, headTip})
	if err != nil {
		return "", false, nil, err
	}
	if err := updateRef(ctx, repoDir, "refs/heads/"+pr.BaseRef, commit, baseTip); err != nil {
		// See the ff path: usually a concurrent push moved base (retryable 409),
		// but log the underlying git error so a real failure stays diagnosable.
		s.logger.Warn("merge update-ref", "repo", repoDir, "ref", pr.BaseRef, "err", err)
		return "", false, nil, errBaseMoved
	}
	return commit, false, nil, nil
}

// rebaseOnto replays every commit the head branch adds over base onto baseTip
// and advances the base ref to the last replayed commit. It is worktree-free:
// each commit is re-applied as a three-way merge computed in memory
// (`merge-tree --merge-base=<original parent>`), then written with commit-tree.
// That is exactly a cherry-pick, done without checking anything out.
//
// It deliberately does not move the head ref. The rebased commits are new
// objects with new SHAs, so rewriting head would rewrite published history for
// anyone who fetched it; the PR is closed as merged instead, exactly as the
// other two methods leave it.
//
// Merge commits in the range are dropped, matching `git rebase`'s own default —
// replaying a merge onto a new base is not well-defined without a strategy the
// caller never picked.
func rebaseOnto(ctx context.Context, repoDir string, pr api.PullRequest, baseTip, headTip string) (newTip string, ff bool, conflicts []string, err error) {
	todo, err := rebaseTodo(ctx, repoDir, baseTip, headTip)
	if err != nil {
		return "", false, nil, err
	}
	if len(todo) == 0 {
		// Head adds no non-merge commit over base — everything it contributes is
		// a merge commit, which rebase drops. Advancing base would mark the PR
		// merged having changed nothing, so say so instead.
		return "", false, nil, errNothingToRebase
	}

	onto := baseTip
	for _, c := range todo {
		tree, cf, terr := cherryPickTree(ctx, repoDir, onto, c)
		if terr != nil {
			return "", false, nil, fmt.Errorf("rebase %s: %w", c.oid, terr)
		}
		if len(cf) > 0 {
			return "", false, cf, nil
		}
		picked, cerr := commitTreeAs(ctx, repoDir, tree, c, onto)
		if cerr != nil {
			return "", false, nil, fmt.Errorf("rebase %s: %w", c.oid, cerr)
		}
		onto = picked
	}

	if err := updateRef(ctx, repoDir, "refs/heads/"+pr.BaseRef, onto, baseTip); err != nil {
		// Same reading as the other two paths: almost always the CAS guard
		// losing to a concurrent push, which is a retryable 409.
		return "", false, nil, errBaseMoved
	}
	// The replayed commits sit directly on baseTip, so the ref moved forward in
	// a straight line — a fast-forward in effect, which is the whole point.
	return onto, true, nil, nil
}

// rebaseCommit is one commit to replay: its OID and the first parent the
// original was built against, which is the merge base for re-applying it.
type rebaseCommit struct {
	oid    string
	parent string
}

// rebaseTodo lists the non-merge commits base..head, oldest first — the same
// set and order `git rebase` would replay.
func rebaseTodo(ctx context.Context, repoDir, base, head string) ([]rebaseCommit, error) {
	out, err := gitOutput(ctx, repoDir, "rev-list", "--reverse", "--no-merges",
		"--format=%H %P", "--no-commit-header", base+".."+head)
	if err != nil {
		return nil, err
	}
	var todo []rebaseCommit
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		c := rebaseCommit{oid: fields[0]}
		if len(fields) > 1 {
			c.parent = fields[1] // first parent; --no-merges guarantees at most one
		}
		todo = append(todo, c)
	}
	return todo, nil
}

// cherryPickTree computes the tree that results from applying c onto onto,
// three-way merging against c's original parent. A root commit (no parent) has
// nothing to diff against, so it is applied against the empty tree.
func cherryPickTree(ctx context.Context, repoDir, onto string, c rebaseCommit) (tree string, conflicts []string, err error) {
	mergeBase := c.parent
	if mergeBase == "" {
		mergeBase = emptyTreeOID
	}
	out, code, runErr := gitRun(ctx, repoDir, "merge-tree", "--write-tree", "-z", "--name-only",
		"--merge-base="+mergeBase, onto, c.oid)
	return parseMergeTree(out, code, runErr)
}

// commitTreeAs writes tree as a commit with parent onto, preserving the
// original commit's message, author, and author date — the rebase contract:
// authorship survives, the committer becomes whoever moved it.
func commitTreeAs(ctx context.Context, repoDir, tree string, c rebaseCommit, onto string) (string, error) {
	meta, err := gitOutput(ctx, repoDir, "show", "-s", "--format=%an%x00%ae%x00%aI%x00%B", c.oid)
	if err != nil {
		return "", err
	}
	parts := strings.SplitN(string(meta), "\x00", 4)
	if len(parts) < 4 {
		return "", fmt.Errorf("unreadable commit metadata for %s", c.oid)
	}
	name, email, date, msg := parts[0], parts[1], parts[2], parts[3]

	cmd := exec.CommandContext(ctx, "git", "commit-tree", tree, "-p", onto, "-m", strings.TrimRight(msg, "\n"))
	cmd.Dir = repoDir
	// Author is preserved verbatim; the committer is the server, matching what a
	// local `git rebase` records.
	cmd.Env = append(cmd.Environ(),
		"GIT_AUTHOR_NAME="+name, "GIT_AUTHOR_EMAIL="+email, "GIT_AUTHOR_DATE="+date,
		"GIT_COMMITTER_NAME=moongit", "GIT_COMMITTER_EMAIL=moongit@moongit.local",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", errors.New("git commit-tree: " + err.Error() + ": " + stderr.String())
	}
	return strings.TrimSpace(string(out)), nil
}

// emptyTreeOID is git's well-known empty tree, used as the merge base when
// replaying a root commit (which has no parent to diff against).
const emptyTreeOID = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// mergeTree runs `git merge-tree --write-tree` to merge head into base in
// memory. On a clean merge it returns the resulting tree OID; on a content
// conflict it returns the conflicting paths (and an empty tree). The -z output
// is: <tree-oid> NUL <conflicted-path> NUL ... NUL "" NUL <info messages>; the
// empty field terminates the conflicted-files section.
func mergeTree(ctx context.Context, repoDir, base, head string) (tree string, conflicts []string, err error) {
	out, code, runErr := gitRun(ctx, repoDir, "merge-tree", "--write-tree", "-z", "--name-only", base, head)
	return parseMergeTree(out, code, runErr)
}

// parseMergeTree decodes one `merge-tree --write-tree -z --name-only` result,
// shared by the merge and rebase paths since both read the same format.
func parseMergeTree(out []byte, code int, runErr error) (tree string, conflicts []string, err error) {
	switch code {
	case 0:
		// Clean: the whole output is the tree OID (with -z, NUL-terminated).
		return strings.Trim(string(out), "\x00\n"), nil, nil
	case 1:
		// Conflict: first field is the (best-effort) tree, then conflicted paths
		// up to the empty terminator field.
		fields := strings.Split(string(out), "\x00")
		for _, f := range fields[1:] {
			if f == "" {
				break
			}
			conflicts = append(conflicts, f)
		}
		if len(conflicts) == 0 {
			conflicts = []string{"(unknown)"}
		}
		return "", conflicts, nil
	default:
		if runErr == nil {
			runErr = fmt.Errorf("merge-tree exited %d", code)
		}
		return "", nil, runErr
	}
}

// commitTree creates a commit object for tree with the given parents and
// message, authored/committed by identity. Returns the new commit OID.
func commitTree(ctx context.Context, repoDir, tree, msg, identity string, parents []string) (string, error) {
	args := []string{"commit-tree", tree}
	for _, p := range parents {
		args = append(args, "-p", p)
	}
	args = append(args, "-m", msg)

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoDir
	// Stamp the merging identity; the email is synthetic (moongit is local-trust
	// and identifies by token name, not email).
	name := identity
	if name == "" {
		name = "moongit"
	}
	cmd.Env = append(cmd.Environ(),
		"GIT_AUTHOR_NAME="+name, "GIT_AUTHOR_EMAIL="+name+"@moongit.local",
		"GIT_COMMITTER_NAME="+name, "GIT_COMMITTER_EMAIL="+name+"@moongit.local",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", errors.New("git commit-tree: " + err.Error() + ": " + stderr.String())
	}
	return strings.TrimSpace(string(out)), nil
}

// updateRef moves ref to newOID only if it currently points at oldOID — the
// compare-and-swap guard against a concurrent receive-pack. A failure (the
// guard didn't hold or the ref vanished) is returned as an error.
func updateRef(ctx context.Context, repoDir, ref, newOID, oldOID string) error {
	_, err := gitOutput(ctx, repoDir, "update-ref", ref, newOID, oldOID)
	return err
}

// revParse resolves a ref/rev to its full OID.
func revParse(ctx context.Context, repoDir, rev string) (string, error) {
	out, err := gitOutput(ctx, repoDir, "rev-parse", "--verify", rev)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// isAncestor reports whether maybeAncestor is an ancestor of descendant (true
// for equal commits). Any error (bad rev, unrelated) is treated as false.
func isAncestor(ctx context.Context, repoDir, maybeAncestor, descendant string) bool {
	cmd := exec.CommandContext(ctx, "git", "merge-base", "--is-ancestor", maybeAncestor, descendant)
	cmd.Dir = repoDir
	return cmd.Run() == nil
}

// gitRun runs git in repoDir and returns stdout, the process exit code, and any
// error that prevented the process from running (not a non-zero exit). Unlike
// gitOutput it does not fold a non-zero exit into an error, so callers can act
// on a graceful non-zero status (e.g. merge-tree's conflict status 1).
func gitRun(ctx context.Context, repoDir string, args ...string) (stdout []byte, code int, err error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return out, ee.ExitCode(), nil
		}
		return out, -1, err
	}
	return out, 0, nil
}

// closingRefsRE matches GitHub-style closing keywords: closes/close/closed,
// fixes/fix/fixed, resolves/resolve/resolved, followed by an issue number.
var closingRefsRE = regexp.MustCompile(`(?i)\b(?:closes?|fixed?|fixes?|resolves?)\s+#(\d+)`)

// parseClosingRefs returns the distinct issue numbers referenced by closing
// keywords (closes #N, fixes #N, resolves #N) in text.
func parseClosingRefs(text string) []int {
	matches := closingRefsRE.FindAllStringSubmatch(text, -1)
	seen := make(map[int]bool)
	var nums []int
	for _, m := range matches {
		n, err := strconv.Atoi(m[1])
		if err != nil || seen[n] {
			continue
		}
		seen[n] = true
		nums = append(nums, n)
	}
	return nums
}

// closeLinkedIssues transitions issues referenced by closing keywords in the
// PR title+body to done. Best-effort: errors are logged and not propagated so
// a bad issue number never fails the merge response.
func (s *Server) closeLinkedIssues(repoID int64, pr api.PullRequest) {
	nums := parseClosingRefs(pr.Title + " " + pr.Body)
	if len(nums) == 0 {
		return
	}
	state := api.IssueDone
	for _, n := range nums {
		if _, err := storage.UpdateIssue(s.db, repoID, n, &state, nil, nil, nil, nil); err != nil {
			s.logger.Warn("close linked issue on PR merge", "issue", n, "err", err)
		}
	}
}
