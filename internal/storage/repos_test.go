package storage

import (
	"testing"

	"github.com/alehatsman/codefort/internal/api"
)

// The repos summary surfaces three at-a-glance metric counts: open PRs,
// unresolved code-review comments, and non-terminal agent runs. Each must
// count only its own domain — merged PRs, resolved comments, terminal/CI runs
// are excluded — and match between GetRepoSummary and ListRepos.
func TestRepoSummaryMetricCounts(t *testing.T) {
	db, repoID := seedRepo(t)

	// 2 open PRs + 1 merged (merged must not count toward open_pulls).
	for i := 0; i < 3; i++ {
		pr, err := CreatePull(db, repoID, api.CreatePullRequest{
			Base: "main", Head: "feat", Title: "pr", Author: "alice",
		})
		if err != nil {
			t.Fatalf("CreatePull %d: %v", i, err)
		}
		if i == 2 {
			if _, err := MarkMerged(db, repoID, pr.Number, "base", "head"); err != nil {
				t.Fatalf("MarkMerged: %v", err)
			}
		}
	}

	// 3 comments, resolve 1 → 2 unresolved count toward open_reviews.
	for i := 0; i < 3; i++ {
		c, err := CreateCodeComment(db, repoID, api.CreateCodeCommentRequest{
			Ref: "main", Path: "f.go", StartLine: 1, EndLine: 1, Body: "nit", Author: "alice",
		})
		if err != nil {
			t.Fatalf("CreateCodeComment %d: %v", i, err)
		}
		if i == 2 {
			if _, err := SetCodeCommentResolved(db, c.ID, true, "alice"); err != nil {
				t.Fatalf("SetCodeCommentResolved: %v", err)
			}
		}
	}

	// 2 non-terminal agent runs (one queued, one claimed→running) → active_agents=2.
	// A finished agent run (terminal) and a CI run must both be excluded.
	n := 1
	for i := 0; i < 2; i++ {
		if _, err := EnqueueRun(db, repoID, NewRun{
			Kind: RunKindAgent, IssueNumber: &n, CommitSHA: "a", Ref: "HEAD", Event: "agent",
		}); err != nil {
			t.Fatalf("EnqueueRun agent %d: %v", i, err)
		}
	}
	if _, err := ClaimNextRunOfKind(db, RunKindAgent, 0); err != nil { // queued → running
		t.Fatalf("ClaimNextRunOfKind: %v", err)
	}
	doneAgent, err := EnqueueRun(db, repoID, NewRun{
		Kind: RunKindAgent, IssueNumber: &n, CommitSHA: "b", Ref: "HEAD", Event: "agent",
	})
	if err != nil {
		t.Fatalf("EnqueueRun done agent: %v", err)
	}
	if err := FinishRun(db, doneAgent.ID, RunSuccess); err != nil {
		t.Fatalf("FinishRun done agent: %v", err)
	}
	ci, err := EnqueueRun(db, repoID, NewRun{CommitSHA: "c", Ref: "refs/heads/main", Event: "push"})
	if err != nil {
		t.Fatalf("EnqueueRun ci: %v", err)
	}
	if err := FinishRun(db, ci.ID, RunSuccess); err != nil {
		t.Fatalf("FinishRun ci: %v", err)
	}

	check := func(label string, r RepoSummary) {
		if r.OpenPulls != 2 {
			t.Errorf("%s OpenPulls = %d, want 2", label, r.OpenPulls)
		}
		if r.OpenReviews != 2 {
			t.Errorf("%s OpenReviews = %d, want 2", label, r.OpenReviews)
		}
		if r.ActiveAgents != 2 {
			t.Errorf("%s ActiveAgents = %d, want 2", label, r.ActiveAgents)
		}
	}

	got, err := GetRepoSummary(db, "alice", "repo")
	if err != nil {
		t.Fatalf("GetRepoSummary: %v", err)
	}
	check("GetRepoSummary", got)

	repos, err := ListRepos(db)
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("ListRepos len = %d, want 1", len(repos))
	}
	check("ListRepos", repos[0])
}

// A repo with no PRs/comments/agent runs reports zero for all three metrics.
func TestRepoSummaryMetricCountsZero(t *testing.T) {
	db, _ := seedRepo(t)

	got, err := GetRepoSummary(db, "alice", "repo")
	if err != nil {
		t.Fatalf("GetRepoSummary: %v", err)
	}
	if got.OpenPulls != 0 || got.OpenReviews != 0 || got.ActiveAgents != 0 {
		t.Errorf("empty repo metrics = (pulls %d, reviews %d, agents %d), want all 0",
			got.OpenPulls, got.OpenReviews, got.ActiveAgents)
	}
}

// A repo with no CI runs reports empty CI status — the repos list renders no
// icon in that case, so the zero value must be distinguishable.
func TestRepoSummaryNoRuns(t *testing.T) {
	db, _ := seedRepo(t)

	got, err := GetRepoSummary(db, "alice", "repo")
	if err != nil {
		t.Fatalf("GetRepoSummary: %v", err)
	}
	if got.CIStatus != "" || got.CINumber != 0 {
		t.Errorf("no-run repo CI = (%q, %d), want (\"\", 0)", got.CIStatus, got.CINumber)
	}
}

// GetRepoSummary and ListRepos surface the status and number of the repo's
// most recent run (highest number), not an older one.
func TestRepoSummarySurfacesLatestRun(t *testing.T) {
	db, repoID := seedRepo(t)

	old, err := EnqueueRun(db, repoID, NewRun{CommitSHA: "a", Ref: "refs/heads/main", Event: "push"})
	if err != nil {
		t.Fatalf("EnqueueRun old: %v", err)
	}
	if err := FinishRun(db, old.ID, RunSuccess); err != nil {
		t.Fatalf("FinishRun old: %v", err)
	}
	latest, err := EnqueueRun(db, repoID, NewRun{CommitSHA: "b", Ref: "refs/heads/main", Event: "push"})
	if err != nil {
		t.Fatalf("EnqueueRun latest: %v", err)
	}
	if err := FinishRun(db, latest.ID, RunFailed); err != nil {
		t.Fatalf("FinishRun latest: %v", err)
	}

	got, err := GetRepoSummary(db, "alice", "repo")
	if err != nil {
		t.Fatalf("GetRepoSummary: %v", err)
	}
	if got.CIStatus != string(RunFailed) || got.CINumber != latest.Number {
		t.Errorf("GetRepoSummary CI = (%q, %d), want (%q, %d)",
			got.CIStatus, got.CINumber, RunFailed, latest.Number)
	}

	repos, err := ListRepos(db)
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("ListRepos len = %d, want 1", len(repos))
	}
	if repos[0].CIStatus != string(RunFailed) || repos[0].CINumber != latest.Number {
		t.Errorf("ListRepos CI = (%q, %d), want (%q, %d)",
			repos[0].CIStatus, repos[0].CINumber, RunFailed, latest.Number)
	}
}

// Agent runs share the ci_runs table but must not drive the repo's CI icon: a
// newer agent run (higher number) must be ignored in favor of the latest
// kind='ci' run, so the icon reflects pipeline automation only.
func TestRepoSummaryIgnoresAgentRuns(t *testing.T) {
	db, repoID := seedRepo(t)

	ci, err := EnqueueRun(db, repoID, NewRun{CommitSHA: "a", Ref: "refs/heads/main", Event: "push"})
	if err != nil {
		t.Fatalf("EnqueueRun ci: %v", err)
	}
	if err := FinishRun(db, ci.ID, RunSuccess); err != nil {
		t.Fatalf("FinishRun ci: %v", err)
	}

	// A later agent run gets a higher number but a different kind.
	n := 1
	agent, err := EnqueueRun(db, repoID, NewRun{
		Kind: RunKindAgent, IssueNumber: &n, CommitSHA: "b", Ref: "HEAD", Event: "agent",
	})
	if err != nil {
		t.Fatalf("EnqueueRun agent: %v", err)
	}
	if err := FinishRun(db, agent.ID, RunFailed); err != nil {
		t.Fatalf("FinishRun agent: %v", err)
	}

	got, err := GetRepoSummary(db, "alice", "repo")
	if err != nil {
		t.Fatalf("GetRepoSummary: %v", err)
	}
	if got.CIStatus != string(RunSuccess) || got.CINumber != ci.Number {
		t.Errorf("GetRepoSummary CI = (%q, %d), want (%q, %d) — agent run leaked into CI status",
			got.CIStatus, got.CINumber, RunSuccess, ci.Number)
	}

	repos, err := ListRepos(db)
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("ListRepos len = %d, want 1", len(repos))
	}
	if repos[0].CIStatus != string(RunSuccess) || repos[0].CINumber != ci.Number {
		t.Errorf("ListRepos CI = (%q, %d), want (%q, %d) — agent run leaked into CI status",
			repos[0].CIStatus, repos[0].CINumber, RunSuccess, ci.Number)
	}
}
