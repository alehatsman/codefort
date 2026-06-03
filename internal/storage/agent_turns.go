package storage

import (
	"database/sql"
	"errors"
	"time"
)

// TurnStatus is the dispatch lifecycle of one agent turn.
type TurnStatus string

const (
	TurnPending TurnStatus = "pending"
	TurnRunning TurnStatus = "running"
	TurnDone    TurnStatus = "done"
	TurnError   TurnStatus = "error"
)

// ErrNoTurnPending is returned by ClaimNextTurn when nothing is claimable — a
// normal idle condition, not an error to surface.
var ErrNoTurnPending = errors.New("no turn pending")

// AgentTurn is one human message in an agent run's conversation, dispatched as
// its own claude --resume turn. Turn 1 (the issue body) is executed inline and
// not stored; these are the follow-ups.
type AgentTurn struct {
	ID         int64
	RunID      int64
	Seq        int
	Author     string
	Body       string
	Status     TurnStatus
	ClaimedAt  *time.Time
	CreatedAt  time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
}

// EnqueueTurn appends a pending follow-up turn to a run, allocating the next
// per-run seq. Callers gate on the run being an agent run in a state that
// accepts input; this just records the message.
func EnqueueTurn(db *sql.DB, runID int64, author, body string) (AgentTurn, error) {
	tx, err := db.Begin()
	if err != nil {
		return AgentTurn{}, err
	}
	defer tx.Rollback()

	var next int
	if err := tx.QueryRow(
		"SELECT COALESCE(MAX(seq), 0) + 1 FROM agent_turns WHERE run_id = ?", runID,
	).Scan(&next); err != nil {
		return AgentTurn{}, err
	}
	turn, err := scanTurn(tx.QueryRow(`
		INSERT INTO agent_turns(run_id, seq, author, body, status)
		VALUES (?, ?, ?, ?, ?)
		RETURNING `+turnColumns+`
	`, runID, next, author, body, string(TurnPending)))
	if err != nil {
		return AgentTurn{}, err
	}
	if err := tx.Commit(); err != nil {
		return AgentTurn{}, err
	}
	return turn, nil
}

// ListTurns returns a run's turns in seq order.
func ListTurns(db *sql.DB, runID int64) ([]AgentTurn, error) {
	rows, err := db.Query(`SELECT `+turnColumns+` FROM agent_turns WHERE run_id = ? ORDER BY seq ASC`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	turns := make([]AgentTurn, 0)
	for rows.Next() {
		t, err := scanTurn(rows)
		if err != nil {
			return nil, err
		}
		turns = append(turns, t)
	}
	return turns, rows.Err()
}

// ClaimNextTurn atomically claims the oldest dispatchable turn and flips its run
// awaiting_input -> running, returning both. A turn is dispatchable when its
// run is an agent run awaiting input and the turn is pending, or when a prior
// dispatch crashed and left the turn running past its lease (re-claimable; a
// non-positive lease disables that steal). Returns ErrNoTurnPending when idle.
func ClaimNextTurn(db *sql.DB, lease time.Duration) (AgentTurn, CIRun, error) {
	tx, err := db.Begin()
	if err != nil {
		return AgentTurn{}, CIRun{}, err
	}
	defer tx.Rollback()

	cond := "(t.status = 'pending' AND r.status = 'awaiting_input')"
	args := []any{}
	if lease > 0 {
		cond = "((t.status = 'pending' AND r.status = 'awaiting_input') OR " +
			"(t.status = 'running' AND t.claimed_at <= strftime('%s','now') - ?))"
		args = append(args, int64(lease.Seconds()))
	}

	var turnID, runID int64
	err = tx.QueryRow(`
		SELECT t.id, t.run_id FROM agent_turns t
		  JOIN ci_runs r ON r.id = t.run_id
		 WHERE r.kind = 'agent' AND `+cond+`
		 ORDER BY t.id ASC LIMIT 1`, args...).Scan(&turnID, &runID)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentTurn{}, CIRun{}, ErrNoTurnPending
	}
	if err != nil {
		return AgentTurn{}, CIRun{}, err
	}

	// Re-apply the claimable predicate in the UPDATE as a compare-and-set, so a
	// concurrent claimer can't double-take this turn between the SELECT and the
	// UPDATE — mirroring ClaimNextRunOfKind, and correct even if the single-writer
	// guarantee is ever relaxed. The run-status check rides a correlated subquery
	// since this UPDATE has no join.
	updCond := "status = 'pending' AND (SELECT status FROM ci_runs WHERE id = run_id) = 'awaiting_input'"
	updArgs := []any{turnID}
	if lease > 0 {
		updCond = "(status = 'pending' AND (SELECT status FROM ci_runs WHERE id = run_id) = 'awaiting_input') OR " +
			"(status = 'running' AND claimed_at <= strftime('%s','now') - ?)"
		updArgs = append(updArgs, int64(lease.Seconds()))
	}
	turn, err := scanTurn(tx.QueryRow(`
		UPDATE agent_turns
		   SET status = 'running',
		       claimed_at = strftime('%s','now'),
		       started_at = COALESCE(started_at, strftime('%s','now'))
		 WHERE id = ? AND (`+updCond+`)
		 RETURNING `+turnColumns+`
	`, updArgs...))
	if errors.Is(err, sql.ErrNoRows) {
		// Lost the race between SELECT and UPDATE — report idle, not an error.
		return AgentTurn{}, CIRun{}, ErrNoTurnPending
	}
	if err != nil {
		return AgentTurn{}, CIRun{}, err
	}
	// Flip the run to running for the duration of the turn.
	if _, err := tx.Exec(`UPDATE ci_runs SET status = 'running' WHERE id = ?`, runID); err != nil {
		return AgentTurn{}, CIRun{}, err
	}
	run, err := scanRun(tx.QueryRow(`SELECT `+runColumns+` FROM ci_runs WHERE id = ?`, runID))
	if err != nil {
		return AgentTurn{}, CIRun{}, err
	}
	if err := tx.Commit(); err != nil {
		return AgentTurn{}, CIRun{}, err
	}
	return turn, run, nil
}

// ListExpiredAwaitingRuns returns agent runs parked in awaiting_input whose
// started_at is older than maxAge — the lifetime-expired sessions the reaper
// finalizes (tearing down their held container). A non-positive maxAge
// disables it.
func ListExpiredAwaitingRuns(db *sql.DB, maxAge time.Duration) ([]CIRun, error) {
	if maxAge <= 0 {
		return nil, nil
	}
	rows, err := db.Query(`
		SELECT `+runColumns+` FROM ci_runs
		 WHERE kind = 'agent' AND status = 'awaiting_input'
		   AND started_at IS NOT NULL
		   AND started_at <= strftime('%s','now') - ?
	`, int64(maxAge.Seconds()))
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

// FinishTurn sets a turn's terminal status and finished_at.
func FinishTurn(db *sql.DB, turnID int64, status TurnStatus) error {
	res, err := db.Exec(`
		UPDATE agent_turns SET status = ?, finished_at = strftime('%s','now') WHERE id = ?
	`, string(status), turnID)
	return affected(res, err)
}

// MarkRunAwaitingInput parks an agent run between turns: it transitions a
// running run to awaiting_input, holding its container open until the next turn
// (or a finish / idle-timeout). The CAS on status keeps it from clobbering a
// run another path already finalized.
func MarkRunAwaitingInput(db *sql.DB, runID int64) error {
	res, err := db.Exec(`
		UPDATE ci_runs SET status = ? WHERE id = ? AND status = ?
	`, string(RunAwaitingInput), runID, string(RunRunning))
	return affected(res, err)
}

// MarkRunFinishing accepts a parked agent run: it transitions awaiting_input ->
// finishing, after which the runner performs handoff. The CAS on awaiting_input
// rejects a finish on a run that's mid-turn (running) or already terminal —
// callers get ErrNotFound and can surface a conflict.
func MarkRunFinishing(db *sql.DB, runID int64) error {
	res, err := db.Exec(`
		UPDATE ci_runs SET status = ? WHERE id = ? AND status = ?
	`, string(RunFinishing), runID, string(RunAwaitingInput))
	return affected(res, err)
}

// CancelAgentRun force-cancels an agent run from ANY non-terminal state
// (queued/running/awaiting_input/finishing) — the operator force-stop (#146),
// unlike MarkRunFinishing which only accepts a parked run. Stamps finished_at so
// it's terminal immediately. ErrNotFound means the run was already terminal (or
// not an agent run): nothing to cancel. The CAS makes it safe against a runner
// concurrently claiming/parking the same run — whichever write lands first, the
// other no-ops, and the run ends terminal either way.
func CancelAgentRun(db *sql.DB, runID int64) error {
	res, err := db.Exec(`
		UPDATE ci_runs SET status = ?, finished_at = strftime('%s','now')
		 WHERE id = ? AND kind = 'agent'
		   AND status IN ('queued','running','awaiting_input','finishing')
	`, string(RunCanceled), runID)
	return affected(res, err)
}

// CancelCIRun force-cancels a CI run that is still queued — the operator
// force-stop from the Pipelines view (#296). Only the pre-execution state is
// CAS'd here: a CI run that's already running is finalized by its own executeRun
// goroutine (which owns the single terminal write, since FinishRun has no CAS),
// so the runner signals that goroutine instead of writing canceled here.
// ErrNotFound means the run was already claimed/terminal (or not a CI run):
// nothing to cancel at the storage layer.
func CancelCIRun(db *sql.DB, runID int64) error {
	res, err := db.Exec(`
		UPDATE ci_runs SET status = ?, finished_at = strftime('%s','now')
		 WHERE id = ? AND kind = 'ci' AND status = 'queued'
	`, string(RunCanceled), runID)
	return affected(res, err)
}

// ClaimNextFinishingRun atomically claims one agent run in the finishing state
// for handoff, flipping it finishing -> running so a second runner pass won't
// double-process it (the run goes terminal once handoff completes). Returns
// ErrNoRunQueued when none are finishing.
func ClaimNextFinishingRun(db *sql.DB) (CIRun, error) {
	tx, err := db.Begin()
	if err != nil {
		return CIRun{}, err
	}
	defer tx.Rollback()

	var id int64
	err = tx.QueryRow(
		`SELECT id FROM ci_runs WHERE kind = 'agent' AND status = 'finishing' ORDER BY id ASC LIMIT 1`,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return CIRun{}, ErrNoRunQueued
	}
	if err != nil {
		return CIRun{}, err
	}
	run, err := scanRun(tx.QueryRow(`
		UPDATE ci_runs SET status = 'running'
		 WHERE id = ? AND status = 'finishing'
		 RETURNING `+runColumns+`
	`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return CIRun{}, ErrNoRunQueued
	}
	if err != nil {
		return CIRun{}, err
	}
	return run, tx.Commit()
}

const turnColumns = "id, run_id, seq, author, body, status, claimed_at, created_at, started_at, finished_at"

func scanTurn(s scanner) (AgentTurn, error) {
	var t AgentTurn
	var status string
	var claimed, started, finished sql.NullInt64
	var created int64
	if err := s.Scan(
		&t.ID, &t.RunID, &t.Seq, &t.Author, &t.Body, &status,
		&claimed, &created, &started, &finished,
	); err != nil {
		return t, err
	}
	t.Status = TurnStatus(status)
	t.CreatedAt = time.Unix(created, 0).UTC()
	t.ClaimedAt = nullTime(claimed)
	t.StartedAt = nullTime(started)
	t.FinishedAt = nullTime(finished)
	return t, nil
}
