package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/alehatsman/codefort/internal/api"
	"github.com/alehatsman/codefort/internal/config"
	"github.com/alehatsman/codefort/internal/storage"
)

// initBareRepo builds a real bare repo with one commit on main (file f.txt),
// returning the bare path and the base commit SHA.
func initBareRepo(t *testing.T, reposDir string) (bare, base string) {
	t.Helper()
	work := t.TempDir()
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Alice", "GIT_AUTHOR_EMAIL=a@b.c",
			"GIT_COMMITTER_NAME=Alice", "GIT_COMMITTER_EMAIL=a@b.c",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git(work, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(work, "f.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(work, "add", ".")
	git(work, "commit", "-q", "-m", "init")

	bare = filepath.Join(reposDir, "alice", "repo.git")
	if err := os.MkdirAll(filepath.Dir(bare), 0o755); err != nil {
		t.Fatal(err)
	}
	git(work, "clone", "-q", "--bare", work, bare)
	base = git(work, "rev-parse", "HEAD")
	return bare, base
}

func gitIn(t *testing.T, bare string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"--git-dir", bare}, args...)...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// handoffHarness wires a runner over a real bare repo with a base commit, an
// agent run parked, its job, and a workspace seeded from base plus an edit.
func handoffHarness(t *testing.T, withEdit bool) (*ciRunner, storage.CIRun, *api.Issue, *atomic.Int32) {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.Open(filepath.Join(dir, "ci.db"))
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
	reposDir := filepath.Join(dir, "repos")
	_, base := initBareRepo(t, reposDir)

	issue, err := storage.CreateIssue(db, repoID, api.CreateIssueRequest{Title: "Add a thing", Author: "alice"})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	n := issue.Number
	run, err := storage.EnqueueRun(db, repoID, storage.NewRun{
		Kind: storage.RunKindAgent, IssueNumber: &n, CommitSHA: base, Ref: "HEAD", Event: "agent",
	})
	if err != nil {
		t.Fatalf("EnqueueRun: %v", err)
	}
	job, err := storage.CreateJob(db, run.ID, agentJobName, nil)
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	_ = job

	// Seed the workspace from base (f.txt) + optionally the agent's edit.
	workDir := agentWorkDir(dir, run.ID)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "f.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if withEdit {
		if err := os.WriteFile(filepath.Join(workDir, "new.txt"), []byte("agent was here\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var teardowns atomic.Int32
	r := &ciRunner{
		db:                db,
		cfg:               &config.Config{DataDir: dir, ReposDir: reposDir},
		logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		teardownContainer: func(string) { teardowns.Add(1) },
	}
	return r, run, &issue, &teardowns
}

// A finished run with edits materializes agent/issue-N in the bare repo, posts
// a summary comment, finalizes success, and tears down.
func TestFinishAgentRunMaterializesBranch(t *testing.T) {
	r, run, issue, teardowns := handoffHarness(t, true)
	bare := filepath.Join(r.cfg.ReposDir, "alice", "repo.git")

	r.finishAgentRun(context.Background(), run)

	// Branch exists and carries the agent's new file.
	commit, err := gitIn(t, bare, "rev-parse", "refs/heads/agent/issue-1")
	if err != nil {
		t.Fatalf("branch agent/issue-1 not created: %v", err)
	}
	files, err := gitIn(t, bare, "ls-tree", "--name-only", commit)
	if err != nil {
		t.Fatalf("ls-tree: %v", err)
	}
	if !strings.Contains(files, "new.txt") {
		t.Errorf("branch tree = %q, want it to include new.txt", files)
	}

	// A summary comment naming the branch is posted on the issue.
	comments, err := storage.ListComments(r.db, issue.ID)
	if err != nil || len(comments) != 1 {
		t.Fatalf("ListComments = %v, %v; want one comment", comments, err)
	}
	if comments[0].Author != agentCommentAuthor || !strings.Contains(comments[0].Body, "agent/issue-1") {
		t.Errorf("comment = %+v, want agent author naming the branch", comments[0])
	}

	if got := r.status(t, run); got != storage.RunSuccess {
		t.Errorf("run status = %q, want success", got)
	}
	if teardowns.Load() != 1 {
		t.Errorf("teardowns = %d, want 1", teardowns.Load())
	}
}

// A finished run with no edits makes no branch and says so.
func TestFinishAgentRunNoChanges(t *testing.T) {
	r, run, issue, _ := handoffHarness(t, false)
	bare := filepath.Join(r.cfg.ReposDir, "alice", "repo.git")

	r.finishAgentRun(context.Background(), run)

	if _, err := gitIn(t, bare, "rev-parse", "--verify", "-q", "refs/heads/agent/issue-1"); err == nil {
		t.Error("no-change run should not create a branch")
	}
	comments, _ := storage.ListComments(r.db, issue.ID)
	if len(comments) != 1 || !strings.Contains(comments[0].Body, "no file changes") {
		t.Errorf("comment = %+v, want a no-changes note", comments)
	}
	if got := r.status(t, run); got != storage.RunSuccess {
		t.Errorf("run status = %q, want success", got)
	}
}

// wireAgentMoongitRemote adds a `codefort` remote (the server URL) to the
// already-cloned agent workspace so in-container cf can resolve owner/repo,
// without disturbing the clone's local-path `origin` (#144). An empty URL is a
// no-op; a re-entry re-points an existing `codefort` remote.
func TestWireAgentMoongitRemote(t *testing.T) {
	remoteURL := func(t *testing.T, workDir, name string) (string, error) {
		t.Helper()
		cmd := exec.Command("git", "remote", "get-url", name)
		cmd.Env = append(os.Environ(), "GIT_DIR="+filepath.Join(workDir, ".git"), "GIT_WORK_TREE="+workDir)
		out, err := cmd.Output()
		return strings.TrimSpace(string(out)), err
	}

	// clonedWorkspace mimics gitCheckout: a real repo whose `origin` is the bare
	// repo's local filesystem path (no URL scheme — the thing cf chokes on).
	clonedWorkspace := func(t *testing.T) (workDir, originPath string) {
		t.Helper()
		bare := t.TempDir()
		run := func(args ...string) {
			t.Helper()
			cmd := exec.Command("git", args...)
			cmd.Env = append(os.Environ(),
				"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v (%s)", args, err, out)
			}
		}
		seed := t.TempDir()
		run("-C", seed, "init", "-q", "-b", "main")
		if err := os.WriteFile(filepath.Join(seed, "f.txt"), []byte("hi\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		run("-C", seed, "add", "-A")
		run("-C", seed, "commit", "-q", "-m", "seed")
		run("clone", "-q", "--bare", seed, bare)
		workDir = t.TempDir()
		run("clone", "-q", "--local", bare, workDir)
		return workDir, bare
	}

	t.Run("adds codefort remote, leaves origin untouched", func(t *testing.T) {
		workDir, originPath := clonedWorkspace(t)
		want := "http://host.docker.internal:8080/alice/repo.git"
		if err := wireAgentMoongitRemote(context.Background(), workDir, want); err != nil {
			t.Fatalf("wireAgentMoongitRemote: %v", err)
		}
		if got, err := remoteURL(t, workDir, "codefort"); err != nil || got != want {
			t.Errorf("codefort = %q, %v; want %q", got, err, want)
		}
		// The clone's local-path origin is preserved (and is still scheme-less,
		// which is exactly why we don't rely on it).
		if got, err := remoteURL(t, workDir, "origin"); err != nil || got != originPath {
			t.Errorf("origin = %q, %v; want %q (clone's local path, untouched)", got, err, originPath)
		}
	})

	t.Run("idempotent — re-entry re-points codefort", func(t *testing.T) {
		workDir, _ := clonedWorkspace(t)
		first := "http://host.docker.internal:8080/alice/repo.git"
		second := "http://host.docker.internal:9090/alice/repo.git"
		if err := wireAgentMoongitRemote(context.Background(), workDir, first); err != nil {
			t.Fatalf("first: %v", err)
		}
		if err := wireAgentMoongitRemote(context.Background(), workDir, second); err != nil {
			t.Fatalf("second: %v", err)
		}
		if got, err := remoteURL(t, workDir, "codefort"); err != nil || got != second {
			t.Errorf("codefort = %q, %v; want %q", got, err, second)
		}
	})

	t.Run("empty URL is a no-op", func(t *testing.T) {
		workDir, _ := clonedWorkspace(t)
		if err := wireAgentMoongitRemote(context.Background(), workDir, ""); err != nil {
			t.Fatalf("wireAgentMoongitRemote: %v", err)
		}
		if got, err := remoteURL(t, workDir, "codefort"); err == nil {
			t.Errorf("expected no codefort remote, got %q", got)
		}
	})
}

// status reads a run's current status.
func (r *ciRunner) status(t *testing.T, run storage.CIRun) storage.RunStatus {
	t.Helper()
	got, err := storage.GetRun(r.db, run.RepoID, run.Number)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	return got.Status
}

// A handoff must never clobber an existing branch in its series. Each run
// commits on its own immutable base, so an existing tip is never an ancestor of
// the new commit — a force-write here would be true history loss, not a
// fast-forward (#197).
func TestFinishAgentRunDoesNotClobberEarlierHandoff(t *testing.T) {
	r, run, issue, _ := handoffHarness(t, true)
	bare := filepath.Join(r.cfg.ReposDir, "alice", "repo.git")

	// Stand in for an earlier run's handoff, pointing somewhere this run would
	// never produce.
	if _, err := gitIn(t, bare, "update-ref", "refs/heads/agent/issue-1", run.CommitSHA); err != nil {
		t.Fatalf("seed prior handoff: %v", err)
	}

	r.finishAgentRun(context.Background(), run)

	// The earlier branch is exactly where it was.
	if got, err := gitIn(t, bare, "rev-parse", "refs/heads/agent/issue-1"); err != nil || got != run.CommitSHA {
		t.Errorf("agent/issue-1 = %q (err %v), want it untouched at %q", got, err, run.CommitSHA)
	}
	// This run's work landed on the next name in the series instead.
	next, err := gitIn(t, bare, "rev-parse", "refs/heads/agent/issue-1-2")
	if err != nil {
		t.Fatalf("agent/issue-1-2 not created: %v", err)
	}
	files, err := gitIn(t, bare, "ls-tree", "--name-only", next)
	if err != nil || !strings.Contains(files, "new.txt") {
		t.Errorf("agent/issue-1-2 tree = %q (err %v), want it to include new.txt", files, err)
	}

	// The comment must name the branch that was actually taken, or a reviewer
	// looks at the wrong one.
	comments, err := storage.ListComments(r.db, issue.ID)
	if err != nil || len(comments) != 1 {
		t.Fatalf("ListComments = %v, %v; want one comment", comments, err)
	}
	if !strings.Contains(comments[0].Body, "agent/issue-1-2") {
		t.Errorf("comment body = %q, want it to name agent/issue-1-2", comments[0].Body)
	}
}

func TestAllocateHandoffRefTakesNextFreeName(t *testing.T) {
	reposDir := t.TempDir()
	bare, base := initBareRepo(t, reposDir)
	env := append(os.Environ(), "GIT_DIR="+bare)

	const refBase = "refs/heads/agent/issue-7"
	first, err := allocateHandoffRef(context.Background(), env, refBase, base)
	if err != nil {
		t.Fatalf("first allocate: %v", err)
	}
	if first != refBase {
		t.Errorf("first ref = %q, want %q", first, refBase)
	}

	// The series steps aside rather than overwriting.
	second, err := allocateHandoffRef(context.Background(), env, refBase, base)
	if err != nil {
		t.Fatalf("second allocate: %v", err)
	}
	if second != refBase+"-2" {
		t.Errorf("second ref = %q, want %q", second, refBase+"-2")
	}
	third, err := allocateHandoffRef(context.Background(), env, refBase, base)
	if err != nil {
		t.Fatalf("third allocate: %v", err)
	}
	if third != refBase+"-3" {
		t.Errorf("third ref = %q, want %q", third, refBase+"-3")
	}

	// And the original still resolves — the whole point.
	if got, err := gitIn(t, bare, "rev-parse", refBase); err != nil || got != base {
		t.Errorf("%s = %q (err %v), want %q", refBase, got, err, base)
	}
}

// A genuine git failure must surface, not be mistaken for "this name is taken"
// and silently consume the whole series.
func TestAllocateHandoffRefReportsRealFailure(t *testing.T) {
	reposDir := t.TempDir()
	bare, base := initBareRepo(t, reposDir)
	env := append(os.Environ(), "GIT_DIR="+bare)

	// "refs/heads" alone is not a valid ref name, so update-ref fails and
	// show-ref finds nothing — the shape of a real fault.
	if _, err := allocateHandoffRef(context.Background(), env, "refs/heads", base); err == nil {
		t.Error("allocateHandoffRef on an invalid ref name = nil error, want a failure")
	}
}
