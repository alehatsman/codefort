package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"

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

	// Nothing to merge if head is already contained in base (head is an ancestor
	// of base, which also covers base == head).
	if isAncestor(r.Context(), repoDir, headTip, baseTip) {
		writeError(w, http.StatusConflict, "nothing to merge: head is already in base")
		return
	}

	newTip, ff, conflicts, err := s.doMerge(r.Context(), repoDir, pr, req.Method, baseTip, headTip, identityFromContext(r))
	switch {
	case errors.Is(err, errNotFastForward):
		writeError(w, http.StatusConflict, "not fast-forwardable; use method \"merge\" or rebase head onto base")
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
	writeJSON(w, http.StatusOK, api.MergeResult{
		PullRequest: updated,
		MergeCommit: newTip,
		FastForward: ff,
	})
}

// errNotFastForward / errBaseMoved are sentinels doMerge returns so the handler
// can map them to specific 409 messages.
var (
	errNotFastForward = errors.New("not fast-forwardable")
	errBaseMoved      = errors.New("base ref moved")
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
		return headTip, true, nil, nil
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

// mergeTree runs `git merge-tree --write-tree` to merge head into base in
// memory. On a clean merge it returns the resulting tree OID; on a content
// conflict it returns the conflicting paths (and an empty tree). The -z output
// is: <tree-oid> NUL <conflicted-path> NUL ... NUL "" NUL <info messages>; the
// empty field terminates the conflicted-files section.
func mergeTree(ctx context.Context, repoDir, base, head string) (tree string, conflicts []string, err error) {
	out, code, runErr := gitRun(ctx, repoDir, "merge-tree", "--write-tree", "-z", "--name-only", base, head)
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
