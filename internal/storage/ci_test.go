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

func TestEnqueueRunFreezesCommitContext(t *testing.T) {
	db, repoID := seedRepo(t)
	run, err := EnqueueRun(db, repoID, NewRun{
		CommitSHA: "abc", CommitMsg: "fix: thing", CommitAuthor: "Alice",
		Ref: "refs/heads/main", Event: "push",
	})
	if err != nil {
		t.Fatalf("EnqueueRun: %v", err)
	}
	got, err := GetRun(db, repoID, run.Number)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.CommitMsg != "fix: thing" || got.CommitAuthor != "Alice" {
		t.Errorf("commit context = (%q, %q), want (%q, %q)",
			got.CommitMsg, got.CommitAuthor, "fix: thing", "Alice")
	}
}

func TestCreateJobRoundTripsNeeds(t *testing.T) {
	db, repoID := seedRepo(t)
	run, err := EnqueueRun(db, repoID, NewRun{CommitSHA: "a", Ref: "r", Event: "push"})
	if err != nil {
		t.Fatalf("EnqueueRun: %v", err)
	}
	if _, err := CreateJob(db, run.ID, "build", nil); err != nil {
		t.Fatalf("CreateJob build: %v", err)
	}
	if _, err := CreateJob(db, run.ID, "test", []string{"build"}); err != nil {
		t.Fatalf("CreateJob test: %v", err)
	}

	jobs, err := ListJobs(db, run.ID)
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	byName := map[string]CIJob{}
	for _, j := range jobs {
		byName[j.Name] = j
	}
	// A root job carries no needs (nil, not []string{}); a dependent one carries
	// exactly its declared dependencies.
	if byName["build"].Needs != nil {
		t.Errorf("build needs = %v, want nil", byName["build"].Needs)
	}
	if got := byName["test"].Needs; len(got) != 1 || got[0] != "build" {
		t.Errorf("test needs = %v, want [build]", got)
	}
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

	job, err := CreateJob(db, run.ID, "test", nil)
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if job.Status != JobQueued || job.ExitCode != nil {
		t.Errorf("new job = %q exit=%v, want queued + nil exit", job.Status, job.ExitCode)
	}
	if err := StartJob(db, job.ID); err != nil {
		t.Fatalf("StartJob: %v", err)
	}
	code := 0
	if err := FinishJob(db, job.ID, JobSuccess, &code); err != nil {
		t.Fatalf("FinishJob: %v", err)
	}

	// Re-creating the same job (the lease-reclaim path for an orphaned run)
	// is idempotent: it resets the existing row to queued and clears the prior
	// run's start/finish/exit instead of colliding on UNIQUE(run_id, name).
	reset, err := CreateJob(db, run.ID, "test", nil)
	if err != nil {
		t.Fatalf("CreateJob (re-create): %v", err)
	}
	if reset.ID != job.ID {
		t.Errorf("re-create made a new row id=%d, want reuse of %d", reset.ID, job.ID)
	}
	if reset.Status != JobQueued || reset.ExitCode != nil || reset.StartedAt != nil || reset.FinishedAt != nil {
		t.Errorf("re-created job = %q exit=%v started=%v finished=%v, want queued + cleared",
			reset.Status, reset.ExitCode, reset.StartedAt, reset.FinishedAt)
	}
	if jobs, _ := ListJobs(db, run.ID); len(jobs) != 1 {
		t.Fatalf("after re-create jobs = %d, want 1", len(jobs))
	}

	// Re-run it so the assertions below still see a terminal, fully-stamped job.
	if err := StartJob(db, job.ID); err != nil {
		t.Fatalf("StartJob: %v", err)
	}
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

func TestReconcileOrphanRuns(t *testing.T) {
	db, repoID := seedRepo(t)

	// An orphaned run: stuck 'running' with a running job and two queued jobs,
	// the state a moongitd restart strands mid-run.
	orphan, _ := EnqueueRun(db, repoID, NewRun{CommitSHA: "a", Ref: "r", Event: "push"})
	if _, err := ClaimNextRun(db, time.Hour); err != nil {
		t.Fatalf("ClaimNextRun: %v", err)
	}
	building, _ := CreateJob(db, orphan.ID, "build", nil)
	CreateJob(db, orphan.ID, "test", []string{"build"})
	CreateJob(db, orphan.ID, "vet", []string{"build"})
	if err := StartJob(db, building.ID); err != nil {
		t.Fatalf("StartJob: %v", err)
	}

	// A separately queued run must be left untouched.
	queued, _ := EnqueueRun(db, repoID, NewRun{CommitSHA: "b", Ref: "r", Event: "push"})

	n, err := ReconcileOrphanRuns(db)
	if err != nil {
		t.Fatalf("ReconcileOrphanRuns: %v", err)
	}
	if n != 1 {
		t.Fatalf("reconciled = %d, want 1", n)
	}

	got, _ := GetRun(db, repoID, orphan.Number)
	if got.Status != RunError || got.FinishedAt == nil {
		t.Errorf("orphan run = %q finished=%v, want error + finished_at", got.Status, got.FinishedAt)
	}
	jobs, _ := ListJobs(db, orphan.ID)
	want := map[string]JobStatus{"build": JobError, "test": JobSkipped, "vet": JobSkipped}
	for _, j := range jobs {
		if j.Status != want[j.Name] {
			t.Errorf("job %q = %q, want %q", j.Name, j.Status, want[j.Name])
		}
		if j.FinishedAt == nil {
			t.Errorf("job %q should have finished_at set", j.Name)
		}
	}

	if q, _ := GetRun(db, repoID, queued.Number); q.Status != RunQueued {
		t.Errorf("queued run = %q, want left queued", q.Status)
	}

	// Idempotent: a second pass finds nothing to reconcile.
	if n, _ := ReconcileOrphanRuns(db); n != 0 {
		t.Errorf("second pass reconciled = %d, want 0", n)
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
