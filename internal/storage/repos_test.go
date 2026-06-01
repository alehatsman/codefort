package storage

import "testing"

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
