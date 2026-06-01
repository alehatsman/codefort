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

	"github.com/alehatsman/moongit/internal/api"
	"github.com/alehatsman/moongit/internal/config"
	"github.com/alehatsman/moongit/internal/storage"
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

// initAgentGitRepo makes /work a real repo with a base commit and, given a
// non-empty originURL, wires it as the `origin` remote so in-container mgit can
// resolve owner/repo (#120). An empty URL leaves the repo remote-less.
func TestInitAgentGitRepoWiresOrigin(t *testing.T) {
	remoteURL := func(t *testing.T, workDir string) (string, error) {
		t.Helper()
		cmd := exec.Command("git", "remote", "get-url", "origin")
		cmd.Env = append(os.Environ(), "GIT_DIR="+filepath.Join(workDir, ".git"), "GIT_WORK_TREE="+workDir)
		out, err := cmd.Output()
		return strings.TrimSpace(string(out)), err
	}

	t.Run("origin set to the repo URL", func(t *testing.T) {
		workDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(workDir, "f.txt"), []byte("hi\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		want := "http://host.docker.internal:8080/alice/repo.git"
		if err := initAgentGitRepo(context.Background(), workDir, want); err != nil {
			t.Fatalf("initAgentGitRepo: %v", err)
		}
		// A base commit exists (the pilot needs git history).
		head := exec.Command("git", "rev-parse", "HEAD")
		head.Env = append(os.Environ(), "GIT_DIR="+filepath.Join(workDir, ".git"))
		if out, err := head.Output(); err != nil || strings.TrimSpace(string(out)) == "" {
			t.Fatalf("no base commit: %v", err)
		}
		got, err := remoteURL(t, workDir)
		if err != nil || got != want {
			t.Errorf("origin = %q, %v; want %q", got, err, want)
		}
	})

	t.Run("empty URL leaves no remote", func(t *testing.T) {
		workDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(workDir, "f.txt"), []byte("hi\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := initAgentGitRepo(context.Background(), workDir, ""); err != nil {
			t.Fatalf("initAgentGitRepo: %v", err)
		}
		if got, err := remoteURL(t, workDir); err == nil {
			t.Errorf("expected no origin remote, got %q", got)
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
