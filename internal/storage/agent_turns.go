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

	turn, err := scanTurn(tx.QueryRow(`
		UPDATE agent_turns
		   SET status = 'running',
		       claimed_at = strftime('%s','now'),
		       started_at = COALESCE(started_at, strftime('%s','now'))
		 WHERE id = ?
		 RETURNING `+turnColumns+`
	`, turnID))
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
