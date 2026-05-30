package storage

import (
	"database/sql"
	"errors"
	"testing"
)

// finishedRun enqueues a run and drives it to a terminal status.
func finishedRun(t *testing.T, db *sql.DB, repoID int64, sha string) CIRun {
	t.Helper()
	run, err := EnqueueRun(db, repoID, NewRun{CommitSHA: sha, Ref: "refs/heads/main", Event: "push"})
	if err != nil {
		t.Fatalf("EnqueueRun: %v", err)
	}
	if err := FinishRun(db, run.ID, RunSuccess); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}
	return run
}

func runExists(t *testing.T, db *sql.DB, repoID int64, number int) bool {
	t.Helper()
	_, err := GetRun(db, repoID, number)
	switch {
	case err == nil:
		return true
	case errors.Is(err, ErrNotFound):
		return false
	default:
		t.Fatalf("GetRun: %v", err)
		return false
	}
}

func TestPruneRunsKeepsNewestDropsOlder(t *testing.T) {
	db, repoID := seedRepo(t)
	for i := 1; i <= 5; i++ {
		finishedRun(t, db, repoID, "sha")
	}
	// Give run #1 a job so we can confirm the child rows go too.
	r1, _ := GetRun(db, repoID, 1)
	if _, err := CreateJob(db, r1.ID, "build", nil); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	pruned, err := PruneRuns(db, 3) // keep newest 3 (#3,#4,#5); drop #1,#2
	if err != nil {
		t.Fatalf("PruneRuns: %v", err)
	}
	if len(pruned) != 2 {
		t.Fatalf("pruned %d runs, want 2 (#1,#2): %+v", len(pruned), pruned)
	}
	for _, p := range pruned {
		if p.Owner != "alice" || p.Repo != "repo" {
			t.Errorf("pruned run identity = %s/%s, want alice/repo", p.Owner, p.Repo)
		}
		if p.Number != 1 && p.Number != 2 {
			t.Errorf("pruned run #%d, want only #1 or #2", p.Number)
		}
	}
	for n := 1; n <= 2; n++ {
		if runExists(t, db, repoID, n) {
			t.Errorf("run #%d still present, should be pruned", n)
		}
	}
	for n := 3; n <= 5; n++ {
		if !runExists(t, db, repoID, n) {
			t.Errorf("run #%d missing, should be retained", n)
		}
	}
	// The pruned run's jobs are gone too.
	var jobs int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ci_jobs WHERE run_id = ?`, r1.ID).Scan(&jobs); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if jobs != 0 {
		t.Errorf("pruned run kept %d job rows, want 0", jobs)
	}
}

func TestPruneRunsNeverPrunesNonTerminal(t *testing.T) {
	db, repoID := seedRepo(t)
	// #1 stays queued (never finished) despite being the oldest; #2..#5 finish.
	if _, err := EnqueueRun(db, repoID, NewRun{CommitSHA: "q", Ref: "r", Event: "push"}); err != nil {
		t.Fatalf("EnqueueRun: %v", err)
	}
	for i := 2; i <= 5; i++ {
		finishedRun(t, db, repoID, "sha")
	}

	pruned, err := PruneRuns(db, 2) // keep newest 2 (#4,#5); beyond = #1,#2,#3
	if err != nil {
		t.Fatalf("PruneRuns: %v", err)
	}
	// #1 is queued → protected; only the terminal #2,#3 are pruned.
	if len(pruned) != 2 {
		t.Fatalf("pruned %d, want 2 (#2,#3): %+v", len(pruned), pruned)
	}
	if !runExists(t, db, repoID, 1) {
		t.Error("queued run #1 was pruned, must be protected")
	}
	for _, n := range []int{2, 3} {
		if runExists(t, db, repoID, n) {
			t.Errorf("terminal run #%d should be pruned", n)
		}
	}
}

func TestPruneRunsDisabledIsNoop(t *testing.T) {
	db, repoID := seedRepo(t)
	for i := 1; i <= 3; i++ {
		finishedRun(t, db, repoID, "sha")
	}
	pruned, err := PruneRuns(db, 0)
	if err != nil {
		t.Fatalf("PruneRuns: %v", err)
	}
	if pruned != nil {
		t.Errorf("disabled retention pruned %+v, want nil", pruned)
	}
	for n := 1; n <= 3; n++ {
		if !runExists(t, db, repoID, n) {
			t.Errorf("run #%d removed while retention disabled", n)
		}
	}
}

func TestPruneRunsKeepsAllWhenUnderLimit(t *testing.T) {
	db, repoID := seedRepo(t)
	for i := 1; i <= 3; i++ {
		finishedRun(t, db, repoID, "sha")
	}
	pruned, err := PruneRuns(db, 10) // keep 10, only 3 exist
	if err != nil {
		t.Fatalf("PruneRuns: %v", err)
	}
	if pruned != nil {
		t.Errorf("pruned %+v with fewer runs than the limit, want nil", pruned)
	}
}
