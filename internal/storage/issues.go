package storage

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/alehatsman/moongit/internal/api"
)

// ErrAlreadyClaimed is returned by Claim when the target issue already has
// an assignee. Distinct from ErrNotFound so the handler can map to 409.
var ErrAlreadyClaimed = errors.New("issue already claimed")

// CreateIssue allocates the next per-repo issue number and inserts the row.
// The (repo_id, number) UNIQUE constraint + SQLite's single-writer guarantee
// keep numbering monotonic without explicit locking.
func CreateIssue(db *sql.DB, repoID int64, req api.CreateIssueRequest) (api.Issue, error) {
	tx, err := db.Begin()
	if err != nil {
		return api.Issue{}, err
	}
	defer tx.Rollback()

	var next int
	if err := tx.QueryRow(
		"SELECT COALESCE(MAX(number), 0) + 1 FROM issues WHERE repo_id = ?", repoID,
	).Scan(&next); err != nil {
		return api.Issue{}, err
	}

	row := tx.QueryRow(`
		INSERT INTO issues(repo_id, number, title, body, author, state)
		VALUES (?, ?, ?, ?, ?, ?)
		RETURNING `+issueColumns+`
	`, repoID, next, req.Title, req.Body, req.Author, string(api.IssueTodo))

	iss, err := scanIssue(row)
	if err != nil {
		return api.Issue{}, err
	}
	if err := tx.Commit(); err != nil {
		return api.Issue{}, err
	}
	return iss, nil
}

func GetIssue(db *sql.DB, repoID int64, number int) (api.Issue, error) {
	row := db.QueryRow(`SELECT `+issueColumns+` FROM issues WHERE repo_id = ? AND number = ?`, repoID, number)
	iss, err := scanIssue(row)
	if errors.Is(err, sql.ErrNoRows) {
		return iss, ErrNotFound
	}
	return iss, err
}

// ErrNoUpdateFields is returned by UpdateIssue when every field is nil —
// there's nothing to change. The handler maps it to 400.
var ErrNoUpdateFields = errors.New("no fields to update")

// UpdateIssue applies a partial update: only the non-nil fields are written,
// and updated_at is bumped. Returns the updated row, ErrNotFound if
// (repoID, number) doesn't exist, or ErrNoUpdateFields if nothing was given.
// Validation (state values, non-empty title) is the caller's responsibility.
func UpdateIssue(db *sql.DB, repoID int64, number int, state *api.IssueState, title, body *string) (api.Issue, error) {
	sets := []string{"updated_at = strftime('%s', 'now')"}
	args := []any{}
	if state != nil {
		sets = append(sets, "state = ?")
		args = append(args, string(*state))
	}
	if title != nil {
		sets = append(sets, "title = ?")
		args = append(args, *title)
	}
	if body != nil {
		sets = append(sets, "body = ?")
		args = append(args, *body)
	}
	// Only the bumped updated_at — caller passed no real fields.
	if len(sets) == 1 {
		return api.Issue{}, ErrNoUpdateFields
	}
	args = append(args, repoID, number)

	row := db.QueryRow(`
		UPDATE issues
		   SET `+strings.Join(sets, ", ")+`
		 WHERE repo_id = ? AND number = ?
		 RETURNING `+issueColumns+`
	`, args...)
	iss, err := scanIssue(row)
	if errors.Is(err, sql.ErrNoRows) {
		return iss, ErrNotFound
	}
	return iss, err
}

// Claim atomically takes ownership of an issue, stamping claimed_at as the
// lease start. It succeeds when the issue is unclaimed, when the same
// assignee re-claims (heartbeat — refreshes the lease), or when the existing
// claim is older than lease (orphaned by a crashed agent). A non-positive
// lease disables expiry: only unclaimed issues and heartbeats succeed.
// Returns ErrAlreadyClaimed if a live claim is held by someone else,
// ErrNotFound if the issue doesn't exist.
func Claim(db *sql.DB, repoID int64, number int, assignee string, state api.IssueState, lease time.Duration) (api.Issue, error) {
	// Compare-and-set in a single UPDATE — the WHERE clause is the lock.
	// If 0 rows change, distinguish "not found" from "live claim by other".
	tx, err := db.Begin()
	if err != nil {
		return api.Issue{}, err
	}
	defer tx.Rollback()

	var setState string
	args := []any{assignee}
	if state != "" {
		setState = ", state = ?"
		args = append(args, string(state))
	}
	// WHERE args: repo, number, heartbeat-owner, [lease seconds].
	args = append(args, repoID, number, assignee)
	expiry := ""
	if lease > 0 {
		expiry = " OR claimed_at <= strftime('%s','now') - ?"
		args = append(args, int64(lease.Seconds()))
	}

	res, err := tx.Exec(`
		UPDATE issues
		   SET assignee = ?, claimed_at = strftime('%s','now'), updated_at = strftime('%s','now')`+setState+`
		 WHERE repo_id = ? AND number = ?
		   AND (assignee IS NULL OR assignee = ?`+expiry+`)
	`, args...)
	if err != nil {
		return api.Issue{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return api.Issue{}, err
	}
	if n == 0 {
		// Distinguish missing-issue from a live claim held by someone else.
		row := tx.QueryRow(`SELECT 1 FROM issues WHERE repo_id = ? AND number = ?`, repoID, number)
		var one int
		if err := row.Scan(&one); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return api.Issue{}, ErrNotFound
			}
			return api.Issue{}, err
		}
		return api.Issue{}, ErrAlreadyClaimed
	}

	row := tx.QueryRow(`SELECT `+issueColumns+` FROM issues WHERE repo_id = ? AND number = ?`, repoID, number)
	iss, err := scanIssue(row)
	if err != nil {
		return api.Issue{}, err
	}
	return iss, tx.Commit()
}

// Unclaim clears the assignee and claim lease, but only for the current
// owner. Returns ErrNotFound if the issue doesn't exist, ErrNotOwner if a
// different agent holds the claim. Unclaiming an already-unclaimed issue is
// idempotent success (the result is the same regardless of who asks).
func Unclaim(db *sql.DB, repoID int64, number int, caller string) (api.Issue, error) {
	tx, err := db.Begin()
	if err != nil {
		return api.Issue{}, err
	}
	defer tx.Rollback()

	cur, err := scanIssue(tx.QueryRow(
		`SELECT `+issueColumns+` FROM issues WHERE repo_id = ? AND number = ?`, repoID, number))
	if errors.Is(err, sql.ErrNoRows) {
		return api.Issue{}, ErrNotFound
	}
	if err != nil {
		return api.Issue{}, err
	}
	// Already unclaimed — nothing to do, same outcome for any caller.
	if cur.Assignee == nil {
		return cur, tx.Commit()
	}
	if *cur.Assignee != caller {
		return api.Issue{}, ErrNotOwner
	}

	iss, err := scanIssue(tx.QueryRow(`
		UPDATE issues
		   SET assignee = NULL, claimed_at = NULL, updated_at = strftime('%s','now')
		 WHERE repo_id = ? AND number = ?
		 RETURNING `+issueColumns+`
	`, repoID, number))
	if err != nil {
		return api.Issue{}, err
	}
	return iss, tx.Commit()
}

// ExpireClaims releases every claim older than lease, clearing assignee and
// claimed_at so orphaned work (a crashed agent) becomes discoverable as
// unassigned rather than only stealable on the next competing claim. Returns
// the number of claims released. A non-positive lease is a no-op — expiry is
// disabled. State is intentionally left untouched; releasing ownership is the
// reaper's only job.
//
// Terminal states (done/closed) are skipped: their assignee is completion
// attribution ("who did it"), not a live lease, and must survive indefinitely.
func ExpireClaims(db *sql.DB, lease time.Duration) (int64, error) {
	if lease <= 0 {
		return 0, nil
	}
	res, err := db.Exec(`
		UPDATE issues
		   SET assignee = NULL, claimed_at = NULL, updated_at = strftime('%s','now')
		 WHERE assignee IS NOT NULL
		   AND state NOT IN ('done','closed')
		   AND claimed_at <= strftime('%s','now') - ?
	`, int64(lease.Seconds()))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ListFilter narrows the result set for ListIssues. Empty fields are
// ignored (no filter). Assignee == "null" matches unassigned issues
// specifically; an empty Assignee means "any."
type ListFilter struct {
	States   []api.IssueState // OR-match; nil/empty means any
	Assignee string           // "" = any, "null" = unassigned, otherwise exact
	Limit    int              // 0 = default (100), capped at 1000
}

func ListIssues(db *sql.DB, repoID int64, filter ListFilter) ([]api.Issue, error) {
	q := strings.Builder{}
	q.WriteString(`SELECT ` + issueColumns + ` FROM issues WHERE repo_id = ?`)
	args := []any{repoID}

	if len(filter.States) > 0 {
		q.WriteString(" AND state IN (")
		for i, s := range filter.States {
			if i > 0 {
				q.WriteString(",")
			}
			q.WriteString("?")
			args = append(args, string(s))
		}
		q.WriteString(")")
	}
	if filter.Assignee == "null" {
		q.WriteString(" AND assignee IS NULL")
	} else if filter.Assignee != "" {
		q.WriteString(" AND assignee = ?")
		args = append(args, filter.Assignee)
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	q.WriteString(" ORDER BY number DESC LIMIT ?")
	args = append(args, limit)

	rows, err := db.Query(q.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	issues := make([]api.Issue, 0)
	for rows.Next() {
		iss, err := scanIssue(rows)
		if err != nil {
			return nil, err
		}
		issues = append(issues, iss)
	}
	return issues, rows.Err()
}

// issueColumns is the canonical select list, used everywhere so scanIssue
// stays in sync with INSERT/UPDATE RETURNING and SELECT.
const issueColumns = "id, number, title, body, author, state, assignee, claimed_at, created_at, updated_at"

// scanner abstracts *sql.Row and *sql.Rows so scanIssue can serve both.
type scanner interface {
	Scan(dest ...any) error
}

func scanIssue(s scanner) (api.Issue, error) {
	var iss api.Issue
	var assignee sql.NullString
	var claimed sql.NullInt64
	var created, updated int64
	if err := s.Scan(
		&iss.ID, &iss.Number, &iss.Title, &iss.Body, &iss.Author, &iss.State,
		&assignee, &claimed, &created, &updated,
	); err != nil {
		return iss, err
	}
	if assignee.Valid {
		iss.Assignee = &assignee.String
	}
	if claimed.Valid {
		ts := time.Unix(claimed.Int64, 0).UTC()
		iss.ClaimedAt = &ts
	}
	iss.CreatedAt = time.Unix(created, 0).UTC()
	iss.UpdatedAt = time.Unix(updated, 0).UTC()
	return iss, nil
}
