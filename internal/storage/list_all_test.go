package storage

import (
	"database/sql"
	"testing"

	"github.com/alehatsman/moongit/internal/api"
)

// seedTwoRepos returns a migrated DB plus two repos owned by different users,
// for exercising the cross-repo aggregate list functions.
func seedTwoRepos(t *testing.T) (db *sql.DB, alice, bob int64) {
	t.Helper()
	d, repoA := seedRepo(t) // alice/repo
	repoB, err := EnsureRepo(d, "bob", "proj")
	if err != nil {
		t.Fatalf("EnsureRepo bob/proj: %v", err)
	}
	return d, repoA, repoB
}

func TestListAllIssuesAcrossRepos(t *testing.T) {
	db, alice, bob := seedTwoRepos(t)
	mustCreate(t, db, alice, "alice issue", "")
	mustCreate(t, db, bob, "bob issue", "")

	got, err := ListAllIssues(db, ListFilter{})
	if err != nil {
		t.Fatalf("ListAllIssues: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d issues, want 2 (one per repo)", len(got))
	}
	// Every row must be tagged with its owning repo so the UI can link back.
	byRepo := map[string]string{} // "owner/name" -> title
	for _, iss := range got {
		byRepo[iss.Repo.Owner+"/"+iss.Repo.Name] = iss.Title
	}
	if byRepo["alice/repo"] != "alice issue" {
		t.Errorf("alice/repo => %q, want %q", byRepo["alice/repo"], "alice issue")
	}
	if byRepo["bob/proj"] != "bob issue" {
		t.Errorf("bob/proj => %q, want %q", byRepo["bob/proj"], "bob issue")
	}
}

func TestListAllIssuesStateFilter(t *testing.T) {
	db, alice, bob := seedTwoRepos(t)
	mustCreate(t, db, alice, "open one", "")
	if _, err := CreateIssue(db, bob, api.CreateIssueRequest{Title: "to close", Author: "bob"}); err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	// Close bob's issue, then filter to closed — only it should come back.
	closed := api.IssueClosed
	if _, err := UpdateIssue(db, bob, 1, &closed, nil, nil); err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}

	got, err := ListAllIssues(db, ListFilter{States: []api.IssueState{api.IssueClosed}})
	if err != nil {
		t.Fatalf("ListAllIssues: %v", err)
	}
	if len(got) != 1 || got[0].Repo.Name != "proj" || got[0].State != api.IssueClosed {
		t.Fatalf("closed filter => %+v, want bob/proj's closed issue only", got)
	}
}

func TestListAllPullsAcrossRepos(t *testing.T) {
	db, alice, bob := seedTwoRepos(t)
	if _, err := CreatePull(db, alice, api.CreatePullRequest{Base: "main", Head: "feat", Title: "alice pr", Author: "alice"}); err != nil {
		t.Fatalf("CreatePull alice: %v", err)
	}
	if _, err := CreatePull(db, bob, api.CreatePullRequest{Base: "main", Head: "feat", Title: "bob pr", Author: "bob"}); err != nil {
		t.Fatalf("CreatePull bob: %v", err)
	}

	got, err := ListAllPulls(db, nil, "")
	if err != nil {
		t.Fatalf("ListAllPulls: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d PRs, want 2", len(got))
	}
	seen := map[string]bool{}
	for _, pr := range got {
		seen[pr.Repo.Owner+"/"+pr.Repo.Name] = true
	}
	if !seen["alice/repo"] || !seen["bob/proj"] {
		t.Errorf("repos seen = %v, want both alice/repo and bob/proj", seen)
	}

	// State filter narrows to one.
	open, err := ListAllPulls(db, []api.PRState{api.PROpen}, "")
	if err != nil {
		t.Fatalf("ListAllPulls open: %v", err)
	}
	if len(open) != 2 {
		t.Errorf("open PRs = %d, want 2 (both born open)", len(open))
	}
	merged, err := ListAllPulls(db, []api.PRState{api.PRMerged}, "")
	if err != nil {
		t.Fatalf("ListAllPulls merged: %v", err)
	}
	if len(merged) != 0 {
		t.Errorf("merged PRs = %d, want 0", len(merged))
	}

	// Query filter spans repos and matches title case-insensitively.
	q, err := ListAllPulls(db, nil, "ALICE")
	if err != nil {
		t.Fatalf("ListAllPulls query: %v", err)
	}
	if len(q) != 1 || q[0].Repo.Owner != "alice" {
		t.Errorf("query 'ALICE' = %+v, want only alice's PR", q)
	}
}

func TestListAllRunsKindFilter(t *testing.T) {
	db, alice, bob := seedTwoRepos(t)
	if _, err := EnqueueRun(db, alice, NewRun{Kind: RunKindCI, CommitSHA: "a1", Ref: "refs/heads/main", Event: "push"}); err != nil {
		t.Fatalf("EnqueueRun ci: %v", err)
	}
	issueNum := 1
	if _, err := EnqueueRun(db, bob, NewRun{Kind: RunKindAgent, IssueNumber: &issueNum, CommitSHA: "b1", Ref: "refs/heads/main", Event: "agent"}); err != nil {
		t.Fatalf("EnqueueRun agent: %v", err)
	}

	all, err := ListAllRuns(db, "", 0)
	if err != nil {
		t.Fatalf("ListAllRuns: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("all runs = %d, want 2", len(all))
	}

	ci, err := ListAllRuns(db, RunKindCI, 0)
	if err != nil {
		t.Fatalf("ListAllRuns ci: %v", err)
	}
	if len(ci) != 1 || ci[0].Owner != "alice" || ci[0].Run.Kind != RunKindCI {
		t.Fatalf("ci runs => %+v, want alice's single ci run", ci)
	}

	agent, err := ListAllRuns(db, RunKindAgent, 0)
	if err != nil {
		t.Fatalf("ListAllRuns agent: %v", err)
	}
	if len(agent) != 1 || agent[0].Name != "proj" || agent[0].Run.Kind != RunKindAgent {
		t.Fatalf("agent runs => %+v, want bob/proj's single agent run", agent)
	}
}
