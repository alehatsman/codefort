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

// UpdateIssue sets the issue's state and bumps updated_at. Returns the
// updated row, or ErrNotFound if (repoID, number) doesn't exist.
func UpdateIssue(db *sql.DB, repoID int64, number int, state api.IssueState) (api.Issue, error) {
	row := db.QueryRow(`
		UPDATE issues
		   SET state = ?, updated_at = strftime('%s', 'now')
		 WHERE repo_id = ? AND number = ?
		 RETURNING `+issueColumns+`
	`, string(state), repoID, number)
	iss, err := scanIssue(row)
	if errors.Is(err, sql.ErrNoRows) {
		return iss, ErrNotFound
	}
	return iss, err
}

// Claim atomically sets assignee (and optionally state) on an unassigned
// issue. Returns ErrAlreadyClaimed if assignee is already set,
// ErrNotFound if the issue doesn't exist.
func Claim(db *sql.DB, repoID int64, number int, assignee string, state api.IssueState) (api.Issue, error) {
	// Compare-and-set: only update if assignee IS NULL. If 0 rows are
	// affected, distinguish "not found" from "already claimed" by reading.
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
	args = append(args, repoID, number)

	res, err := tx.Exec(`
		UPDATE issues
		   SET assignee = ?`+setState+`, updated_at = strftime('%s','now')
		 WHERE repo_id = ? AND number = ? AND assignee IS NULL
	`, args...)
	if err != nil {
		return api.Issue{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return api.Issue{}, err
	}
	if n == 0 {
		// Distinguish missing-issue from already-claimed.
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

// Unclaim clears the assignee. Idempotent — returns ErrNotFound only when
// the issue doesn't exist, not when it was already unclaimed.
func Unclaim(db *sql.DB, repoID int64, number int) (api.Issue, error) {
	row := db.QueryRow(`
		UPDATE issues
		   SET assignee = NULL, updated_at = strftime('%s','now')
		 WHERE repo_id = ? AND number = ?
		 RETURNING `+issueColumns+`
	`, repoID, number)
	iss, err := scanIssue(row)
	if errors.Is(err, sql.ErrNoRows) {
		return iss, ErrNotFound
	}
	return iss, err
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
const issueColumns = "id, number, title, body, author, state, assignee, created_at, updated_at"

// scanner abstracts *sql.Row and *sql.Rows so scanIssue can serve both.
type scanner interface {
	Scan(dest ...any) error
}

func scanIssue(s scanner) (api.Issue, error) {
	var iss api.Issue
	var assignee sql.NullString
	var created, updated int64
	if err := s.Scan(
		&iss.ID, &iss.Number, &iss.Title, &iss.Body, &iss.Author, &iss.State,
		&assignee, &created, &updated,
	); err != nil {
		return iss, err
	}
	if assignee.Valid {
		iss.Assignee = &assignee.String
	}
	iss.CreatedAt = time.Unix(created, 0).UTC()
	iss.UpdatedAt = time.Unix(updated, 0).UTC()
	return iss, nil
}
