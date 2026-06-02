package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/alehatsman/moongit/internal/api"
	"github.com/alehatsman/moongit/internal/storage"
)

// agentCommentAuthor is the identity the agent's handoff/failure comments are
// posted under.
const agentCommentAuthor = "moongit-agent"

// agentBranchRef is the ref an agent run's work lands on.
func agentBranchRef(issueNumber int) string {
	return fmt.Sprintf("agent/issue-%d", issueNumber)
}

// finishAgentRun performs handoff for a claimed finishing run: materialize the
// workspace as a commit on agent/issue-<n> in the bare repo (server-side — no
// push, since moongitd owns the repo), post a summary comment on the issue,
// tear down the container/workspace/token, and finalize the run. A handoff
// failure finalizes the run errored with a failure comment; the branch ref is
// only updated on a clean materialize, so a failure never leaves a half-pushed
// branch.
func (r *ciRunner) finishAgentRun(parent context.Context, run storage.CIRun) {
	log := r.logger.With("kind", "agent", "run", run.Number, "phase", "handoff")

	owner, name, err := storage.RepoIdent(r.db, run.RepoID)
	if err != nil {
		log.Error("handoff resolve repo", "err", err)
		r.failAgentRun(run.ID, r.agentJobIDOrZero(run.ID), agentWorkDir(r.cfg.DataDir, run.ID))
		return
	}
	if run.IssueNumber == nil {
		log.Error("handoff: run has no issue")
		r.failAgentRun(run.ID, r.agentJobIDOrZero(run.ID), agentWorkDir(r.cfg.DataDir, run.ID))
		return
	}
	issue, err := storage.GetIssue(r.db, run.RepoID, *run.IssueNumber)
	if err != nil {
		log.Error("handoff get issue", "err", err)
		r.failAgentRun(run.ID, r.agentJobIDOrZero(run.ID), agentWorkDir(r.cfg.DataDir, run.ID))
		return
	}

	jobID := r.agentJobIDOrZero(run.ID)
	workDir := agentWorkDir(r.cfg.DataDir, run.ID)
	bareRepo := filepath.Join(r.cfg.ReposDir, owner, name+".git")
	branch := agentBranchRef(issue.Number)

	// The MCP config we wrote into the workspace isn't the agent's work — drop
	// it so it doesn't land in the branch.
	_ = os.Remove(filepath.Join(workDir, agentMCPConfigName))

	msg := fmt.Sprintf("agent: %s\n\nWorked issue #%d via moongit agent run #%d.\n",
		issue.Title, issue.Number, run.Number)
	commit, changed, err := materializeAgentBranch(parent, bareRepo, run.CommitSHA, workDir, "refs/heads/"+branch, agentCommentAuthor, msg)
	if err != nil {
		log.Error("handoff materialize branch", "err", err)
		r.postAgentComment(issue.ID, fmt.Sprintf(
			"⚠️ Agent run [#%d](%s) finished, but handoff failed: %s. The transcript is on the run page.",
			run.Number, r.runLink(owner, name, run.Number), err.Error()))
		r.tearDownAgent(run.ID, jobID, workDir)
		r.finishJob(jobID, storage.JobError, nil)
		r.finish(run, storage.RunError)
		return
	}

	var body string
	if changed {
		stat := agentDiffStat(parent, bareRepo, run.CommitSHA, commit)
		body = fmt.Sprintf(
			"✅ Agent run [#%d](%s) finished. Work is on branch `%s` (commit `%s`).\n\n%s\n\nReview the diff and merge if it looks good — there's no PR object; the deliverable is the branch + this comment.",
			run.Number, r.runLink(owner, name, run.Number), branch, short(commit), stat)
	} else {
		body = fmt.Sprintf(
			"ℹ️ Agent run [#%d](%s) finished with no file changes — nothing to hand off.",
			run.Number, r.runLink(owner, name, run.Number))
	}
	r.postAgentComment(issue.ID, body)

	// Mark the agent job + run done, then tear down.
	if jobID != 0 {
		zero := 0
		r.finishJob(jobID, storage.JobSuccess, &zero)
	}
	r.tearDownAgent(run.ID, jobID, workDir)
	r.finish(run, storage.RunSuccess)
	log.Info("agent run finished", "branch", branch, "changed", changed)
}

// materializeAgentBranch commits the workspace tree onto base as ref, directly
// in the bare repo, and reports whether it differed from base. It uses a throw-
// away index with the bare repo as GIT_DIR and the workspace as GIT_WORK_TREE,
// seeding the index from base so deletions are captured. The ref is updated
// only after a successful commit, so a mid-way failure leaves no branch. When
// the tree is identical to base, no commit/ref is made (changed=false).
func materializeAgentBranch(ctx context.Context, bareRepo, base, workDir, ref, author, msg string) (commit string, changed bool, err error) {
	idx := filepath.Join(os.TempDir(), fmt.Sprintf("moongit-agent-index-%d-%d", os.Getpid(), hashRef(ref)))
	defer func() { _ = os.Remove(idx) }()

	base = strings.TrimSpace(base)
	env := append(os.Environ(),
		"GIT_DIR="+bareRepo,
		"GIT_INDEX_FILE="+idx,
		"GIT_WORK_TREE="+workDir,
	)
	if _, err := runGit(ctx, env, "read-tree", base); err != nil {
		return "", false, fmt.Errorf("read-tree: %w", err)
	}
	// Snapshot the worktree, but exclude the mooncake-agent scratch dir
	// (.mooncake/agent/iterations etc.) — it's per-run executor bookkeeping,
	// not part of the agent's change to the repo. (.git is skipped by git
	// natively.)
	if _, err := runGit(ctx, env, "add", "-A", "--", ".", ":(exclude).mooncake"); err != nil {
		return "", false, fmt.Errorf("add: %w", err)
	}
	tree, err := runGit(ctx, env, "write-tree")
	if err != nil {
		return "", false, fmt.Errorf("write-tree: %w", err)
	}
	baseTree, err := runGit(ctx, env, "rev-parse", base+"^{tree}")
	if err != nil {
		return "", false, fmt.Errorf("rev-parse base tree: %w", err)
	}
	if tree == baseTree {
		return "", false, nil // agent changed nothing
	}
	commitEnv := append(env,
		"GIT_AUTHOR_NAME="+author, "GIT_AUTHOR_EMAIL=agent@moongit.local",
		"GIT_COMMITTER_NAME="+author, "GIT_COMMITTER_EMAIL=agent@moongit.local",
	)
	commit, err = runGit(ctx, commitEnv, "commit-tree", tree, "-p", base, "-m", msg)
	if err != nil {
		return "", false, fmt.Errorf("commit-tree: %w", err)
	}
	if _, err := runGit(ctx, env, "update-ref", ref, commit); err != nil {
		return "", false, fmt.Errorf("update-ref: %w", err)
	}
	return commit, true, nil
}

// agentDiffStat returns a fenced `git diff --stat base..commit`, or "" on error
// (a missing stat shouldn't block the handoff comment).
func agentDiffStat(ctx context.Context, bareRepo, base, commit string) string {
	out, err := runGit(ctx, append(os.Environ(), "GIT_DIR="+bareRepo), "diff", "--stat", strings.TrimSpace(base)+".."+commit)
	if err != nil || strings.TrimSpace(out) == "" {
		return ""
	}
	return "```\n" + out + "\n```"
}

// commentAgentFailure posts a failure/timeout note on the run's issue, linking
// the transcript. Best-effort: it needs the issue, so a pre-issue failure
// simply doesn't comment.
func (r *ciRunner) commentAgentFailure(run storage.CIRun, reason string) {
	if run.IssueNumber == nil {
		return
	}
	owner, name, err := storage.RepoIdent(r.db, run.RepoID)
	if err != nil {
		return
	}
	issue, err := storage.GetIssue(r.db, run.RepoID, *run.IssueNumber)
	if err != nil {
		return
	}
	r.postAgentComment(issue.ID, fmt.Sprintf(
		"⚠️ Agent run [#%d](%s) ended: %s. The transcript is on the run page; no branch was pushed.",
		run.Number, r.runLink(owner, name, run.Number), reason))
}

// postAgentComment posts a comment on an issue under the agent identity,
// best-effort (a comment failure is logged, not fatal to finalization).
func (r *ciRunner) postAgentComment(issueID int64, body string) {
	if _, err := storage.CreateComment(r.db, issueID, api.CreateCommentRequest{
		Author: agentCommentAuthor, Body: body,
	}); err != nil {
		r.logger.Error("agent post comment", "issue_id", issueID, "err", err)
	}
}

// runLink is a relative link to an agent run's page, for use in issue-comment
// markdown (agent runs live under the Agents tab).
func (r *ciRunner) runLink(owner, repo string, runNumber int) string {
	return fmt.Sprintf("/%s/%s/agents/%d", owner, repo, runNumber)
}

// agentJobIDOrZero returns the run's agent job id, or 0 if absent.
func (r *ciRunner) agentJobIDOrZero(runID int64) int64 {
	id, _ := r.agentJobID(runID)
	return id
}

// runGit runs git with a custom environment and returns trimmed stdout.
func runGit(ctx context.Context, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %v (%s)", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// wireAgentMoongitRemote adds a `moongit` remote to the agent workspace so
// in-container `mgit` can resolve owner/repo and claim/comment/set-state on its
// issue exactly like a human checkout.
//
// The workspace is a `git clone --local` of the server-side bare repo
// (gitCheckout), so it's already a real repo with history detached at the base
// commit — the mooncake-agent model's git snapshot/diff/transaction steps work
// against it as-is. But clone sets `origin` to the bare repo's local filesystem
// path (e.g. /…/repos/o/r.git), which mgit's parseRemote can't read — it has no
// URL scheme, so every in-container mgit call failed with
// `unsupported remote scheme ""` (#144). discoverTarget prefers a dedicated
// `moongit` remote over `origin` (cmd/moongit/main.go), so adding one with the
// server URL fixes resolution while leaving the clone's `origin` untouched.
//
// remoteURL empty → no-op (claude-edit doesn't run mgit and needs no remote).
// Idempotent: a re-park/retry that re-enters here just re-points the remote.
// Best-effort — only the mooncake/mgit path depends on it.
func wireAgentMoongitRemote(ctx context.Context, workDir, remoteURL string) error {
	if remoteURL == "" {
		return nil
	}
	env := append(os.Environ(),
		"GIT_DIR="+filepath.Join(workDir, ".git"),
		"GIT_WORK_TREE="+workDir,
	)
	if _, err := runGit(ctx, env, "remote", "add", "moongit", remoteURL); err != nil {
		// Already present (re-park/retry): point it at the current URL instead.
		if _, err2 := runGit(ctx, env, "remote", "set-url", "moongit", remoteURL); err2 != nil {
			return err
		}
	}
	return nil
}

// hashRef is a tiny stable hash of a ref, to keep concurrent index files
// distinct.
func hashRef(s string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}
