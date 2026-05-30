package storage

import (
	"database/sql"
	"errors"
	"time"
)

// RunStatus is the lifecycle state of a CI run. queued -> running -> one of
// the terminal states (success|failed|canceled|error).
type RunStatus string

const (
	RunQueued   RunStatus = "queued"
	RunRunning  RunStatus = "running"
	RunSuccess  RunStatus = "success"
	RunFailed   RunStatus = "failed"
	RunCanceled RunStatus = "canceled"
	RunError    RunStatus = "error" // infrastructure failure (checkout/parse), not a job's non-zero exit
)

// Terminal reports whether the status is a final state (no further transitions).
func (s RunStatus) Terminal() bool {
	switch s {
	case RunSuccess, RunFailed, RunCanceled, RunError:
		return true
	default:
		return false
	}
}

// JobStatus is the lifecycle state of a single job within a run.
type JobStatus string

const (
	JobQueued  JobStatus = "queued"
	JobRunning JobStatus = "running"
	JobSuccess JobStatus = "success"
	JobFailed  JobStatus = "failed"
	JobSkipped JobStatus = "skipped"
	JobError   JobStatus = "error"
)

// ErrNoRunQueued is returned by ClaimNextRun when there is no claimable run.
// It is a normal idle condition for the runner, not an error to surface.
var ErrNoRunQueued = errors.New("no run queued")

// CIRun is one pipeline execution for a repo, identified per-repo by Number.
type CIRun struct {
	ID         int64
	RepoID     int64
	Number     int
	CommitSHA  string
	Ref        string
	Event      string
	Trigger    string
	Status     RunStatus
	ClaimedAt  *time.Time
	CreatedAt  time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
}

// CIJob is one job within a run, identified within the run by Name.
type CIJob struct {
	ID         int64
	RunID      int64
	Name       string
	Status     JobStatus
	ExitCode   *int
	CreatedAt  time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
}

// NewRun holds the fields needed to enqueue a run. Number, status, and
// timestamps are assigned by EnqueueRun.
type NewRun struct {
	CommitSHA string
	Ref       string
	Event     string
	Trigger   string
}

// EnqueueRun allocates the next per-repo run number and inserts a queued run.
// Mirrors CreateIssue: the (repo_id, number) UNIQUE constraint plus SQLite's
// single writer keep numbering monotonic without explicit locking.
func EnqueueRun(db *sql.DB, repoID int64, r NewRun) (CIRun, error) {
	tx, err := db.Begin()
	if err != nil {
		return CIRun{}, err
	}
	defer tx.Rollback()

	var next int
	if err := tx.QueryRow(
		"SELECT COALESCE(MAX(number), 0) + 1 FROM ci_runs WHERE repo_id = ?", repoID,
	).Scan(&next); err != nil {
		return CIRun{}, err
	}

	run, err := scanRun(tx.QueryRow(`
		INSERT INTO ci_runs(repo_id, number, commit_sha, ref, event, trigger, status)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		RETURNING `+runColumns+`
	`, repoID, next, r.CommitSHA, r.Ref, r.Event, r.Trigger, string(RunQueued)))
	if err != nil {
		return CIRun{}, err
	}
	if err := tx.Commit(); err != nil {
		return CIRun{}, err
	}
	return run, nil
}

// GetRun returns a run by per-repo number, or ErrNotFound.
func GetRun(db *sql.DB, repoID int64, number int) (CIRun, error) {
	run, err := scanRun(db.QueryRow(
		`SELECT `+runColumns+` FROM ci_runs WHERE repo_id = ? AND number = ?`, repoID, number))
	if errors.Is(err, sql.ErrNoRows) {
		return CIRun{}, ErrNotFound
	}
	return run, err
}

// ListRuns returns a repo's runs newest-first, capped at limit (default 100,
// max 1000).
func ListRuns(db *sql.DB, repoID int64, limit int) ([]CIRun, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	rows, err := db.Query(
		`SELECT `+runColumns+` FROM ci_runs WHERE repo_id = ? ORDER BY number DESC LIMIT ?`, repoID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	runs := make([]CIRun, 0)
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

// ClaimNextRun atomically claims the oldest claimable run for execution,
// transitioning it queued|expired-running -> running and stamping the lease.
// A 'running' run whose claimed_at is older than lease is treated as orphaned
// (crashed runner) and re-claimable; a non-positive lease disables that steal,
// so only queued runs are claimed. Returns ErrNoRunQueued when nothing is
// claimable. The compare-and-set WHERE clause is the lock, mirroring Claim —
// correct even if the single-writer guarantee is ever relaxed.
func ClaimNextRun(db *sql.DB, lease time.Duration) (CIRun, error) {
	tx, err := db.Begin()
	if err != nil {
		return CIRun{}, err
	}
	defer tx.Rollback()

	// "claimable" = queued, or running but past its lease.
	claimable := "status = 'queued'"
	selArgs := []any{}
	if lease > 0 {
		claimable = "(status = 'queued' OR (status = 'running' AND claimed_at <= strftime('%s','now') - ?))"
		selArgs = append(selArgs, int64(lease.Seconds()))
	}

	var id int64
	err = tx.QueryRow(
		`SELECT id FROM ci_runs WHERE `+claimable+` ORDER BY number ASC LIMIT 1`, selArgs...,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return CIRun{}, ErrNoRunQueued
	}
	if err != nil {
		return CIRun{}, err
	}

	// CAS on that id with the same predicate, so a concurrent claimer can't
	// double-take it. started_at is set once (first claim), preserved on steal.
	updArgs := []any{id}
	updArgs = append(updArgs, selArgs...)
	run, err := scanRun(tx.QueryRow(`
		UPDATE ci_runs
		   SET status = 'running',
		       claimed_at = strftime('%s','now'),
		       started_at = COALESCE(started_at, strftime('%s','now'))
		 WHERE id = ? AND `+claimable+`
		 RETURNING `+runColumns+`
	`, updArgs...))
	if errors.Is(err, sql.ErrNoRows) {
		// Lost the race between SELECT and UPDATE — report idle rather than error.
		return CIRun{}, ErrNoRunQueued
	}
	if err != nil {
		return CIRun{}, err
	}
	return run, tx.Commit()
}

// FinishRun sets a run's terminal status and finished_at. The status must be
// terminal; use it once a run reaches success/failed/canceled/error.
func FinishRun(db *sql.DB, runID int64, status RunStatus) error {
	if !status.Terminal() {
		return errors.New("FinishRun: status must be terminal")
	}
	res, err := db.Exec(`
		UPDATE ci_runs SET status = ?, finished_at = strftime('%s','now') WHERE id = ?
	`, string(status), runID)
	return affected(res, err)
}

// CreateJob inserts a queued job for a run. Mirrors the per-run name UNIQUE
// constraint so a job name can't be enqueued twice in one run.
func CreateJob(db *sql.DB, runID int64, name string) (CIJob, error) {
	job, err := scanJob(db.QueryRow(`
		INSERT INTO ci_jobs(run_id, name, status) VALUES (?, ?, ?)
		RETURNING `+jobColumns+`
	`, runID, name, string(JobQueued)))
	return job, err
}

// StartJob marks a job running and stamps started_at.
func StartJob(db *sql.DB, jobID int64) error {
	res, err := db.Exec(`
		UPDATE ci_jobs SET status = 'running', started_at = strftime('%s','now') WHERE id = ?
	`, jobID)
	return affected(res, err)
}

// FinishJob sets a job's terminal status, its exit code (nil when the job
// never ran a command, e.g. skipped), and finished_at.
func FinishJob(db *sql.DB, jobID int64, status JobStatus, exitCode *int) error {
	res, err := db.Exec(`
		UPDATE ci_jobs SET status = ?, exit_code = ?, finished_at = strftime('%s','now') WHERE id = ?
	`, string(status), exitCode, jobID)
	return affected(res, err)
}

// ListJobs returns a run's jobs in creation order.
func ListJobs(db *sql.DB, runID int64) ([]CIJob, error) {
	rows, err := db.Query(`SELECT `+jobColumns+` FROM ci_jobs WHERE run_id = ? ORDER BY id ASC`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	jobs := make([]CIJob, 0)
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

// RepoCIEnabled reports whether CI is enabled for a repo. Missing repo -> ErrNotFound.
func RepoCIEnabled(db *sql.DB, repoID int64) (bool, error) {
	var enabled int
	err := db.QueryRow(`SELECT ci_enabled FROM repos WHERE id = ?`, repoID).Scan(&enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	return enabled != 0, err
}

// SetRepoCIEnabled flips a repo's CI opt-in. Missing repo -> ErrNotFound.
func SetRepoCIEnabled(db *sql.DB, repoID int64, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	res, err := db.Exec(`UPDATE repos SET ci_enabled = ? WHERE id = ?`, v, repoID)
	return affected(res, err)
}

// affected maps a 0-rows UPDATE to ErrNotFound, so callers get a consistent
// signal when the target row doesn't exist.
func affected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

const runColumns = "id, repo_id, number, commit_sha, ref, event, trigger, status, claimed_at, created_at, started_at, finished_at"

func scanRun(s scanner) (CIRun, error) {
	var r CIRun
	var status string
	var claimed, started, finished sql.NullInt64
	var created int64
	if err := s.Scan(
		&r.ID, &r.RepoID, &r.Number, &r.CommitSHA, &r.Ref, &r.Event, &r.Trigger,
		&status, &claimed, &created, &started, &finished,
	); err != nil {
		return r, err
	}
	r.Status = RunStatus(status)
	r.CreatedAt = time.Unix(created, 0).UTC()
	r.ClaimedAt = nullTime(claimed)
	r.StartedAt = nullTime(started)
	r.FinishedAt = nullTime(finished)
	return r, nil
}

const jobColumns = "id, run_id, name, status, exit_code, created_at, started_at, finished_at"

func scanJob(s scanner) (CIJob, error) {
	var j CIJob
	var status string
	var exit, started, finished sql.NullInt64
	var created int64
	if err := s.Scan(
		&j.ID, &j.RunID, &j.Name, &status, &exit, &created, &started, &finished,
	); err != nil {
		return j, err
	}
	j.Status = JobStatus(status)
	j.CreatedAt = time.Unix(created, 0).UTC()
	if exit.Valid {
		e := int(exit.Int64)
		j.ExitCode = &e
	}
	j.StartedAt = nullTime(started)
	j.FinishedAt = nullTime(finished)
	return j, nil
}

// nullTime converts a nullable unix-seconds column into an optional UTC time.
func nullTime(n sql.NullInt64) *time.Time {
	if !n.Valid {
		return nil
	}
	t := time.Unix(n.Int64, 0).UTC()
	return &t
}
