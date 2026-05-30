package storage

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// seedRepo spins up a migrated temp DB with one repo, returning the writer
// handle and the repo id.
func seedRepo(t *testing.T) (db *sql.DB, repoID int64) {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "ci.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := Migrate(d); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	id, err := EnsureRepo(d, "alice", "repo")
	if err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}
	return d, id
}

func TestEnqueueRunAllocatesPerRepoNumbers(t *testing.T) {
	db, repoID := seedRepo(t)
	for want := 1; want <= 3; want++ {
		run, err := EnqueueRun(db, repoID, NewRun{CommitSHA: "abc", Ref: "refs/heads/main", Event: "push"})
		if err != nil {
			t.Fatalf("EnqueueRun: %v", err)
		}
		if run.Number != want {
			t.Errorf("run number = %d, want %d", run.Number, want)
		}
		if run.Status != RunQueued {
			t.Errorf("status = %q, want queued", run.Status)
		}
		if run.StartedAt != nil || run.FinishedAt != nil || run.ClaimedAt != nil {
			t.Error("queued run should have nil claimed/started/finished")
		}
	}
}

func TestClaimNextRunFIFOAndLifecycle(t *testing.T) {
	db, repoID := seedRepo(t)
	r1, _ := EnqueueRun(db, repoID, NewRun{CommitSHA: "a", Ref: "r", Event: "push"})
	EnqueueRun(db, repoID, NewRun{CommitSHA: "b", Ref: "r", Event: "push"})

	got, err := ClaimNextRun(db, time.Hour)
	if err != nil {
		t.Fatalf("ClaimNextRun: %v", err)
	}
	if got.Number != r1.Number {
		t.Errorf("claimed run %d, want oldest %d", got.Number, r1.Number)
	}
	if got.Status != RunRunning {
		t.Errorf("status = %q, want running", got.Status)
	}
	if got.ClaimedAt == nil || got.StartedAt == nil {
		t.Error("claimed run should have claimed_at and started_at set")
	}

	// Second claim grabs run 2; a third finds nothing (run 1 is running, not expired).
	got2, err := ClaimNextRun(db, time.Hour)
	if err != nil {
		t.Fatalf("second ClaimNextRun: %v", err)
	}
	if got2.Number != 2 {
		t.Errorf("second claim = %d, want 2", got2.Number)
	}
	if _, err := ClaimNextRun(db, time.Hour); !errors.Is(err, ErrNoRunQueued) {
		t.Fatalf("third claim err = %v, want ErrNoRunQueued", err)
	}
}

func TestClaimNextRunStealsExpiredLease(t *testing.T) {
	db, repoID := seedRepo(t)
	run, _ := EnqueueRun(db, repoID, NewRun{CommitSHA: "a", Ref: "r", Event: "push"})
	if _, err := ClaimNextRun(db, time.Hour); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	// Backdate the lease so it looks orphaned by a crashed runner.
	if _, err := db.Exec(
		`UPDATE ci_runs SET claimed_at = strftime('%s','now') - 100 WHERE id = ?`, run.ID); err != nil {
		t.Fatalf("backdate: %v", err)
	}
	// A short lease steals it; a disabled lease (<=0) does not.
	if _, err := ClaimNextRun(db, 0); !errors.Is(err, ErrNoRunQueued) {
		t.Fatalf("zero-lease claim err = %v, want ErrNoRunQueued (no steal)", err)
	}
	got, err := ClaimNextRun(db, 10*time.Second)
	if err != nil {
		t.Fatalf("steal claim: %v", err)
	}
	if got.Number != run.Number {
		t.Errorf("stolen run = %d, want %d", got.Number, run.Number)
	}
}

func TestFinishRunRequiresTerminalStatus(t *testing.T) {
	db, repoID := seedRepo(t)
	run, _ := EnqueueRun(db, repoID, NewRun{CommitSHA: "a", Ref: "r", Event: "push"})
	if err := FinishRun(db, run.ID, RunRunning); err == nil {
		t.Error("FinishRun with non-terminal status should error")
	}
	if err := FinishRun(db, run.ID, RunSuccess); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}
	got, _ := GetRun(db, repoID, run.Number)
	if got.Status != RunSuccess || got.FinishedAt == nil {
		t.Errorf("run = %q finished=%v, want success + finished_at set", got.Status, got.FinishedAt)
	}
}

func TestJobLifecycle(t *testing.T) {
	db, repoID := seedRepo(t)
	run, _ := EnqueueRun(db, repoID, NewRun{CommitSHA: "a", Ref: "r", Event: "push"})

	job, err := CreateJob(db, run.ID, "test")
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if job.Status != JobQueued || job.ExitCode != nil {
		t.Errorf("new job = %q exit=%v, want queued + nil exit", job.Status, job.ExitCode)
	}
	// Duplicate job name in the same run is rejected by the UNIQUE constraint.
	if _, err := CreateJob(db, run.ID, "test"); err == nil {
		t.Error("duplicate job name should violate UNIQUE(run_id, name)")
	}
	if err := StartJob(db, job.ID); err != nil {
		t.Fatalf("StartJob: %v", err)
	}
	code := 0
	if err := FinishJob(db, job.ID, JobSuccess, &code); err != nil {
		t.Fatalf("FinishJob: %v", err)
	}

	jobs, err := ListJobs(db, run.ID)
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("jobs = %d, want 1", len(jobs))
	}
	j := jobs[0]
	if j.Status != JobSuccess || j.ExitCode == nil || *j.ExitCode != 0 {
		t.Errorf("job = %q exit=%v, want success + exit 0", j.Status, j.ExitCode)
	}
	if j.StartedAt == nil || j.FinishedAt == nil {
		t.Error("finished job should have started_at and finished_at")
	}
}

func TestRepoCIEnabledToggle(t *testing.T) {
	db, repoID := seedRepo(t)
	on, err := RepoCIEnabled(db, repoID)
	if err != nil {
		t.Fatalf("RepoCIEnabled: %v", err)
	}
	if on {
		t.Error("ci_enabled should default to false")
	}
	if err := SetRepoCIEnabled(db, repoID, true); err != nil {
		t.Fatalf("SetRepoCIEnabled: %v", err)
	}
	if on, _ := RepoCIEnabled(db, repoID); !on {
		t.Error("ci_enabled should be true after enabling")
	}
	if err := SetRepoCIEnabled(db, 99999, true); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetRepoCIEnabled(missing) = %v, want ErrNotFound", err)
	}
}

func TestGetRunNotFound(t *testing.T) {
	db, repoID := seedRepo(t)
	if _, err := GetRun(db, repoID, 42); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetRun(missing) = %v, want ErrNotFound", err)
	}
}
