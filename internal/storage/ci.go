package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// RunStatus is the lifecycle state of a CI run. queued -> running -> one of
// the terminal states (success|failed|canceled|error).
type RunStatus string

const (
	RunQueued  RunStatus = "queued"
	RunRunning RunStatus = "running"
	// RunAwaitingInput is an agent-only non-terminal state: a turn finished and
	// the run is holding its container open, waiting for the next human turn
	// (or a finish / idle-timeout). CI runs never enter it.
	RunAwaitingInput RunStatus = "awaiting_input"
	// RunFinishing is an agent-only non-terminal state: the human accepted the
	// run, and the runner is performing handoff (materializing the branch +
	// posting the summary) before the run goes terminal.
	RunFinishing RunStatus = "finishing"
	RunSuccess   RunStatus = "success"
	RunFailed    RunStatus = "failed"
	RunCanceled  RunStatus = "canceled"
	RunError     RunStatus = "error" // infrastructure failure (checkout/parse), not a job's non-zero exit
	// RunInterrupted is a run whose work was cut short by the runner going
	// away — a graceful shutdown (deploy/restart) cancelled an in-flight step,
	// or a crash left it orphaned. It is operator-induced, not a gate result,
	// so it's terminal but distinct from RunError/RunFailed: the UI renders it
	// neutral, not red, and the operator can rerun. (RunCanceled stays reserved
	// for a gated run — CI off / no mgitci.yml — which is a different "didn't
	// run" meaning.)
	RunInterrupted RunStatus = "interrupted"
	// RunStalled is an agent-only terminal state: the agent ran to completion
	// but never converged — it hit its iteration cap or kept re-planning
	// without making progress (mooncake stop_reason max_iterations/no_progress/
	// no_change), with no step actually failing. Like RunInterrupted it's
	// neutral, not red, and rerunnable; unlike it, the run wasn't cut short from
	// outside — the agent gave up on its own. Distinct from RunFailed (a step
	// errored) so an operator can tell "couldn't make progress" from "crashed".
	// CI runs never enter it. See moongit #173 / mooncake #77.
	RunStalled RunStatus = "stalled"
)

// Terminal reports whether the status is a final state (no further transitions).
func (s RunStatus) Terminal() bool {
	switch s {
	case RunSuccess, RunFailed, RunCanceled, RunError, RunInterrupted, RunStalled:
		return true
	default:
		return false
	}
}

// CountActiveRuns returns how many of a repo's runs are non-terminal — queued
// or running, plus the agent-only awaiting_input/finishing — i.e. still owned
// by the in-process runner. Repo deletion uses it to refuse (409) while work is
// in flight: the FK cascade would yank the run rows out from under a live
// goroutine. The non-terminal set is the complement of RunStatus.Terminal();
// it's spelled out here because the filter runs in SQL.
func CountActiveRuns(db *sql.DB, repoID int64) (int, error) {
	var n int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM ci_runs
		 WHERE repo_id = ? AND status IN ('queued','running','awaiting_input','finishing')
	`, repoID).Scan(&n)
	return n, err
}

// RunKind distinguishes a normal pipeline run from an agent run. An agent run
// reuses the entire CI run spine but, instead of executing a translated
// mgitci.yml, works an issue via a containerized Claude session (see #74). The
// zero value is treated as RunKindCI so pre-kind rows and callers that don't
// set it keep the historical behavior.
type RunKind string

const (
	RunKindCI    RunKind = "ci"
	RunKindAgent RunKind = "agent"
)

// Agent execution models (#110): the pluggable strategy an agent run uses
// inside its container. Canonical here so storage (default), the server
// (validation), and the runner (executor selection) share one vocabulary.
const (
	ExecModelClaudeEdit    = "claude-edit"
	ExecModelMooncakeAgent = "mooncake-agent"
	// DefaultExecutionModel seeds runs that don't specify one (and the
	// column default for pre-#110 / CI rows).
	DefaultExecutionModel = ExecModelClaudeEdit
)

// Agent tool profiles (#184): which slice of the mgit MCP toolset an agent run
// is allowed to see. Enforcement is shim-side — `mgit mcp --profile <p>` only
// registers the tools the profile permits — so the restriction holds even
// though claude's --allowedTools is unreliable headlessly (#110). The canonical
// tool→profile mapping lives in the shim (mgit owns its own toolset); storage
// only carries the profile name, defaults it, and validates it.
const (
	// ToolProfileFull exposes the entire mgit + dex surface (current behavior).
	ToolProfileFull = "full"
	// ToolProfileReview restricts a run to read tools + review_* (+ issue_comment):
	// the read-only review agent. No issue_claim/set_state/create, no
	// agent_spawn/turn, no pipeline_trigger.
	ToolProfileReview = "review"
	// DefaultToolProfile seeds runs that don't specify one (and the column
	// default for pre-#184 / CI rows).
	DefaultToolProfile = ToolProfileFull
)

// ValidToolProfile reports whether p names a known agent tool profile.
func ValidToolProfile(p string) bool {
	return p == ToolProfileFull || p == ToolProfileReview
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
	// JobInterrupted mirrors RunInterrupted at the job level: the job's step
	// was cancelled by a runner shutdown, or it was still queued/running when
	// the runner went away. Not a gate failure.
	JobInterrupted JobStatus = "interrupted"
)

// ErrNoRunQueued is returned by ClaimNextRun when there is no claimable run.
// It is a normal idle condition for the runner, not an error to surface.
var ErrNoRunQueued = errors.New("no run queued")

// CIRun is one pipeline execution for a repo, identified per-repo by Number.
type CIRun struct {
	ID          int64
	RepoID      int64
	Number      int
	Kind        RunKind // ci (default) | agent
	IssueNumber *int    // the issue an agent run serves; nil for CI runs
	// ExecutionModel is the agent run's pluggable execution model
	// ('claude-edit' | 'mooncake-agent'); 'claude-edit' for CI rows by
	// the column default, unused by CI (#110).
	ExecutionModel string
	// MooncakeAllowShell, when set at spawn, drops the default shell/cmd denial
	// from the mooncake-agent policy for this run only (#110). Ignored by
	// claude-edit / CI.
	MooncakeAllowShell bool
	// ToolProfile names the mgit MCP toolset slice this agent run sees
	// ('full' | 'review'); 'full' for CI rows by the column default (#184).
	ToolProfile  string
	CommitSHA    string
	CommitMsg    string // commit subject, frozen at enqueue (may be empty)
	CommitAuthor string // commit author name, frozen at enqueue (may be empty)
	Ref          string
	Event        string
	Trigger      string
	Status       RunStatus
	ClaimedAt    *time.Time
	CreatedAt    time.Time
	StartedAt    *time.Time
	FinishedAt   *time.Time
}

// CIJob is one job within a run, identified within the run by Name.
type CIJob struct {
	ID         int64
	RunID      int64
	Name       string
	Needs      []string // jobs this one depends on; nil for a root job
	Status     JobStatus
	ExitCode   *int
	CreatedAt  time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
}

// NewRun holds the fields needed to enqueue a run. Number, status, and
// timestamps are assigned by EnqueueRun. Kind defaults to RunKindCI when empty;
// IssueNumber is set only for agent runs.
type NewRun struct {
	Kind        RunKind
	IssueNumber *int
	// ExecutionModel is the agent execution model; empty falls back to the
	// column default ('claude-edit'). Set only for agent runs (#110).
	ExecutionModel string
	// MooncakeAllowShell overrides the default shell/cmd denial for this
	// mooncake-agent run (#110). Set only for agent runs.
	MooncakeAllowShell bool
	// ToolProfile names the mgit MCP toolset slice for this agent run
	// ('full' | 'review'); empty falls back to the column default ('full').
	// Set only for agent runs (#184).
	ToolProfile  string
	CommitSHA    string
	CommitMsg    string
	CommitAuthor string
	Ref          string
	Event        string
	Trigger      string
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

	kind := r.Kind
	if kind == "" {
		kind = RunKindCI
	}
	execModel := r.ExecutionModel
	if execModel == "" {
		execModel = DefaultExecutionModel
	}
	allowShell := 0
	if r.MooncakeAllowShell {
		allowShell = 1
	}
	profile := r.ToolProfile
	if profile == "" {
		profile = DefaultToolProfile
	}
	run, err := scanRun(tx.QueryRow(`
		INSERT INTO ci_runs(repo_id, number, kind, issue_number, execution_model, mooncake_allow_shell, tool_profile, commit_sha, commit_msg, commit_author, ref, event, trigger, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING `+runColumns+`
	`, repoID, next, string(kind), r.IssueNumber, execModel, allowShell, profile, r.CommitSHA, r.CommitMsg, r.CommitAuthor, r.Ref, r.Event, r.Trigger, string(RunQueued)))
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

// RunFilter narrows a runs list query. Kind ("" = any) picks the run kind;
// Statuses (empty = any) keeps only runs in one of the given lifecycle states;
// Query is a case-insensitive keyword matched against commit subject/author,
// ref, and trigger. Limit defaults to 100, capped at 1000. It mirrors the
// issues ListFilter so the Agents views can filter like the Issues views.
type RunFilter struct {
	Kind     RunKind
	Statuses []RunStatus
	Query    string
	Limit    int
}

func (f RunFilter) clampLimit() int {
	if f.Limit <= 0 {
		return 100
	}
	if f.Limit > 1000 {
		return 1000
	}
	return f.Limit
}

// appendRunFilters adds the kind/status/query clauses shared by ListRuns and
// the cross-repo ListAllRuns. The columns it references are unique to ci_runs,
// so the clauses stay correct unqualified even under the repos/users join the
// aggregate query adds (matching appendIssueFilters).
func appendRunFilters(q *strings.Builder, args *[]any, f RunFilter) {
	if f.Kind != "" {
		q.WriteString(" AND kind = ?")
		*args = append(*args, string(f.Kind))
	}
	if len(f.Statuses) > 0 {
		q.WriteString(" AND status IN (")
		for i, s := range f.Statuses {
			if i > 0 {
				q.WriteString(",")
			}
			q.WriteString("?")
			*args = append(*args, string(s))
		}
		q.WriteString(")")
	}
	if f.Query != "" {
		// %term% against the commit subject/author, ref, and trigger; wildcards
		// in the term are escaped so they're taken literally (as in issues).
		pat := "%" + likeEscape(f.Query) + "%"
		q.WriteString(` AND (commit_msg LIKE ? ESCAPE '\' OR commit_author LIKE ? ESCAPE '\' OR ref LIKE ? ESCAPE '\' OR trigger LIKE ? ESCAPE '\')`)
		*args = append(*args, pat, pat, pat, pat)
	}
}

// ListRuns returns a repo's runs newest-first, capped at filter.Limit (default
// 100, max 1000). The kind/status/query filters are applied in SQL, so the cap
// applies to the matching set — a ?kind=agent page can't come back short
// because the newest `limit` rows happened to be CI runs.
func ListRuns(db *sql.DB, repoID int64, filter RunFilter) ([]CIRun, error) {
	limit := filter.clampLimit()
	q := strings.Builder{}
	q.WriteString(`SELECT ` + runColumns + ` FROM ci_runs WHERE repo_id = ?`)
	args := []any{repoID}
	appendRunFilters(&q, &args, filter)
	q.WriteString(" ORDER BY number DESC LIMIT ?")
	args = append(args, limit)
	rows, err := db.Query(q.String(), args...)
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

// ClaimNextRun atomically claims the oldest claimable CI run. It is the
// kind-defaulting entry point; agent runs are claimed via ClaimNextRunOfKind so
// the runner can pool the two kinds under separate concurrency caps.
func ClaimNextRun(db *sql.DB, lease time.Duration) (CIRun, error) {
	return ClaimNextRunOfKind(db, RunKindCI, lease)
}

// ClaimNextRunOfKind atomically claims the oldest claimable run of the given
// kind for execution, transitioning it queued|expired-running -> running and
// stamping the lease. A 'running' run whose claimed_at is older than lease is
// treated as orphaned (crashed runner) and re-claimable; a non-positive lease
// disables that steal, so only queued runs are claimed. Scoping by kind lets
// agent and CI runs drain from independent pools — a flood of one kind never
// starves the other. Returns ErrNoRunQueued when nothing is claimable. The
// compare-and-set WHERE clause is the lock, mirroring Claim — correct even if
// the single-writer guarantee is ever relaxed.
func ClaimNextRunOfKind(db *sql.DB, kind RunKind, lease time.Duration) (CIRun, error) {
	tx, err := db.Begin()
	if err != nil {
		return CIRun{}, err
	}
	defer tx.Rollback()

	// "claimable" = right kind, and queued or running-but-past-its-lease.
	claimable := "kind = ? AND status = 'queued'"
	selArgs := []any{string(kind)}
	if lease > 0 {
		claimable = "kind = ? AND (status = 'queued' OR (status = 'running' AND claimed_at <= strftime('%s','now') - ?))"
		selArgs = append(selArgs, int64(lease.Seconds()))
	}

	var id int64
	err = tx.QueryRow(
		`SELECT id FROM ci_runs WHERE `+claimable+` ORDER BY id ASC LIMIT 1`, selArgs...,
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

// ReconcileOrphanRuns finalizes runs left in flight with no live runner — the
// classic orphan a moongitd restart strands (the in-flight goroutine dies
// before its status writes commit; an agent run's detached container is swept
// at startup too). It must be called at startup, before the runner takes new
// work, when no run can legitimately be in flight: every 'running' run — and
// every agent run parked in 'awaiting_input', whose container the startup
// sweep just removed — is therefore an orphan. Each is marked interrupted
// (the runner went away mid-flight — operator-induced, not a gate failure),
// its running and still-queued jobs likewise interrupted, and its un-finished
// turns errored, so the UI shows a neutral terminal result instead of
// something stuck forever — or a misleading red. Returns the number of runs
// reconciled.
func ReconcileOrphanRuns(db *sql.DB) (int, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// Orphans are runs left running or (agent-only) awaiting_input/finishing.
	const orphanRuns = `SELECT id FROM ci_runs WHERE status IN ('running','awaiting_input','finishing')`

	// A running or still-queued job in an orphaned run never reached a verdict —
	// it was cut short, not failed or dependency-skipped — so both land
	// interrupted.
	if _, err := tx.Exec(`
		UPDATE ci_jobs SET status = ?, finished_at = strftime('%s','now')
		 WHERE status IN (?, ?) AND run_id IN (`+orphanRuns+`)
	`, string(JobInterrupted), string(JobRunning), string(JobQueued)); err != nil {
		return 0, err
	}
	// Error any pending/running turns of the orphaned agent runs.
	if _, err := tx.Exec(`
		UPDATE agent_turns SET status = ?, finished_at = strftime('%s','now')
		 WHERE status IN ('pending','running') AND run_id IN (`+orphanRuns+`)
	`, string(TurnError)); err != nil {
		return 0, err
	}
	res, err := tx.Exec(`
		UPDATE ci_runs SET status = ?, finished_at = strftime('%s','now')
		 WHERE status IN ('running','awaiting_input','finishing')
	`, string(RunInterrupted))
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(n), tx.Commit()
}

// PrunableRun identifies a pruned run so the caller can drop its on-disk
// event-log directory after the rows are gone.
type PrunableRun struct {
	Owner  string
	Repo   string
	Number int
}

// PruneRuns enforces per-repo run retention: it deletes terminal runs that
// have at least `keep` newer runs in the same repo (i.e. those beyond the
// newest `keep`), removing their ci_jobs and ci_runs rows in one writer
// transaction. queued/running runs are never deleted — and since they carry
// the highest numbers they always fall within the kept window anyway. A
// non-positive `keep` disables retention (no-op, nil result). Returns the
// pruned runs so the caller can delete their on-disk event logs.
func PruneRuns(db *sql.DB, keep int) ([]PrunableRun, error) {
	if keep <= 0 {
		return nil, nil
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// A run is prunable when it is terminal and at least `keep` runs in its repo
	// have a higher number (so it sits beyond the newest `keep`). The newer
	// count includes queued/running runs, which is what we want: they occupy the
	// most-recent slots.
	rows, err := tx.Query(`
		SELECT r.id, u.name, rep.name, r.number
		  FROM ci_runs r
		  JOIN repos rep ON rep.id = r.repo_id
		  JOIN users u   ON u.id   = rep.owner_id
		 WHERE r.status IN ('success','failed','canceled','error')
		   AND (SELECT COUNT(*) FROM ci_runs n
		         WHERE n.repo_id = r.repo_id AND n.number > r.number) >= ?
	`, keep)
	if err != nil {
		return nil, err
	}
	var ids []int64
	var pruned []PrunableRun
	for rows.Next() {
		var id int64
		var p PrunableRun
		if err := rows.Scan(&id, &p.Owner, &p.Repo, &p.Number); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
		pruned = append(pruned, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close() // release the cursor before issuing writes on the same tx

	if len(ids) == 0 {
		return nil, nil
	}
	// Delete children explicitly rather than leaning on the FK cascade, matching
	// DeleteIssue — the behavior holds even if the foreign_keys pragma is off.
	for _, id := range ids {
		if _, err := tx.Exec(`DELETE FROM ci_jobs WHERE run_id = ?`, id); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(`DELETE FROM ci_runs WHERE id = ?`, id); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return pruned, nil
}

// CreateJob inserts a queued job for a run, recording the jobs it `needs` (the
// DAG edges, stored as a JSON array) so the run-detail view can reconstruct
// the dependency structure. It is idempotent on the per-run name UNIQUE
// constraint: re-creating a job (e.g. when an orphaned run is re-claimed via
// the lease steal) resets the existing row back to queued — clearing the prior
// run's start/finish/exit — rather than colliding, so the run re-executes
// cleanly from scratch.
func CreateJob(db *sql.DB, runID int64, name string, needs []string) (CIJob, error) {
	job, err := scanJob(db.QueryRow(`
		INSERT INTO ci_jobs(run_id, name, needs, status) VALUES (?, ?, ?, ?)
		ON CONFLICT(run_id, name) DO UPDATE SET
			needs       = excluded.needs,
			status      = excluded.status,
			exit_code   = NULL,
			started_at  = NULL,
			finished_at = NULL
		RETURNING `+jobColumns+`
	`, runID, name, marshalNeeds(needs), string(JobQueued)))
	return job, err
}

// marshalNeeds encodes a job's dependency list for storage. An empty list is
// stored as "" (the column default) rather than "[]", so a root job's row
// carries no JSON.
func marshalNeeds(needs []string) string {
	if len(needs) == 0 {
		return ""
	}
	b, err := json.Marshal(needs)
	if err != nil {
		return ""
	}
	return string(b)
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

const runColumns = "id, repo_id, number, kind, issue_number, execution_model, mooncake_allow_shell, tool_profile, commit_sha, commit_msg, commit_author, ref, event, trigger, status, claimed_at, created_at, started_at, finished_at"

func scanRun(s scanner) (CIRun, error) {
	var r CIRun
	var kind, status string
	var issueNum, claimed, started, finished sql.NullInt64
	var created int64
	var allowShell int
	if err := s.Scan(
		&r.ID, &r.RepoID, &r.Number, &kind, &issueNum, &r.ExecutionModel, &allowShell, &r.ToolProfile, &r.CommitSHA, &r.CommitMsg, &r.CommitAuthor,
		&r.Ref, &r.Event, &r.Trigger,
		&status, &claimed, &created, &started, &finished,
	); err != nil {
		return r, err
	}
	decodeRun(&r, kind, status, issueNum, claimed, started, finished, created, allowShell)
	return r, nil
}

// decodeRun fills a CIRun's typed/nullable fields from the raw column values,
// shared by scanRun and the cross-repo aggregate scan so they never drift.
func decodeRun(r *CIRun, kind, status string, issueNum, claimed, started, finished sql.NullInt64, created int64, allowShell int) {
	r.Kind = RunKind(kind)
	r.MooncakeAllowShell = allowShell != 0
	if issueNum.Valid {
		n := int(issueNum.Int64)
		r.IssueNumber = &n
	}
	r.Status = RunStatus(status)
	r.CreatedAt = time.Unix(created, 0).UTC()
	r.ClaimedAt = nullTime(claimed)
	r.StartedAt = nullTime(started)
	r.FinishedAt = nullTime(finished)
}

// RunWithRepo pairs a CIRun with its owning repo's owner/name, the shape
// ListAllRuns returns so the server can attach a RepoRef when serializing.
type RunWithRepo struct {
	Run   CIRun
	Owner string
	Name  string
}

// ListAllRuns returns runs across every repo, newest-created first, each tagged
// with its owning repo. It backs GET /api/runs. The kind/status/query filters
// behave as in ListRuns so the fleet-wide Pipelines (ci) and Agents (agent)
// tabs each show only their own and can search/filter. limit defaults to 100,
// capped at 1000. Ordering is by created_at (per-repo run numbers aren't
// globally orderable), id breaking ties.
func ListAllRuns(db *sql.DB, filter RunFilter) ([]RunWithRepo, error) {
	limit := filter.clampLimit()
	q := strings.Builder{}
	// Qualify with ci_runs.: id/repo_id/created_at also exist on the joined
	// repos/users tables, so a bare runColumns would be ambiguous. The filter
	// clauses reference columns unique to ci_runs, so they stay unqualified.
	q.WriteString(`SELECT ci_runs.id, ci_runs.repo_id, ci_runs.number, ci_runs.kind, ci_runs.issue_number, ci_runs.execution_model, ci_runs.mooncake_allow_shell, ci_runs.tool_profile, ci_runs.commit_sha, ci_runs.commit_msg, ci_runs.commit_author, ci_runs.ref, ci_runs.event, ci_runs.trigger, ci_runs.status, ci_runs.claimed_at, ci_runs.created_at, ci_runs.started_at, ci_runs.finished_at, users.name, repos.name
		FROM ci_runs
		JOIN repos ON repos.id = ci_runs.repo_id
		JOIN users ON users.id = repos.owner_id
		WHERE 1=1`)
	args := []any{}
	appendRunFilters(&q, &args, filter)
	q.WriteString(" ORDER BY ci_runs.created_at DESC, ci_runs.id DESC LIMIT ?")
	args = append(args, limit)

	rows, err := db.Query(q.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]RunWithRepo, 0)
	for rows.Next() {
		var r CIRun
		var kindCol, status string
		var issueNum, claimed, started, finished sql.NullInt64
		var created int64
		var allowShell int
		var owner, name string
		if err := rows.Scan(
			&r.ID, &r.RepoID, &r.Number, &kindCol, &issueNum, &r.ExecutionModel, &allowShell, &r.ToolProfile, &r.CommitSHA, &r.CommitMsg, &r.CommitAuthor,
			&r.Ref, &r.Event, &r.Trigger,
			&status, &claimed, &created, &started, &finished, &owner, &name,
		); err != nil {
			return nil, err
		}
		decodeRun(&r, kindCol, status, issueNum, claimed, started, finished, created, allowShell)
		out = append(out, RunWithRepo{Run: r, Owner: owner, Name: name})
	}
	return out, rows.Err()
}

const jobColumns = "id, run_id, name, needs, status, exit_code, created_at, started_at, finished_at"

func scanJob(s scanner) (CIJob, error) {
	var j CIJob
	var status, needs string
	var exit, started, finished sql.NullInt64
	var created int64
	if err := s.Scan(
		&j.ID, &j.RunID, &j.Name, &needs, &status, &exit, &created, &started, &finished,
	); err != nil {
		return j, err
	}
	if needs != "" {
		if err := json.Unmarshal([]byte(needs), &j.Needs); err != nil {
			return j, err
		}
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
