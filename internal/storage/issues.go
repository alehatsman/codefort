package storage

import (
	"database/sql"
	"errors"
	"time"

	"github.com/alehatsman/moongit/internal/api"
)

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
		RETURNING id, number, title, body, author, state, created_at, updated_at
	`, repoID, next, req.Title, req.Body, req.Author, string(api.IssueOpen))

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
	row := db.QueryRow(`
		SELECT id, number, title, body, author, state, created_at, updated_at
		FROM issues WHERE repo_id = ? AND number = ?
	`, repoID, number)
	iss, err := scanIssue(row)
	if errors.Is(err, sql.ErrNoRows) {
		return iss, ErrNotFound
	}
	return iss, err
}

func ListIssues(db *sql.DB, repoID int64) ([]api.Issue, error) {
	rows, err := db.Query(`
		SELECT id, number, title, body, author, state, created_at, updated_at
		FROM issues WHERE repo_id = ? ORDER BY number DESC
	`, repoID)
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

// scanner abstracts *sql.Row and *sql.Rows so scanIssue can serve both.
type scanner interface {
	Scan(dest ...any) error
}

func scanIssue(s scanner) (api.Issue, error) {
	var iss api.Issue
	var created, updated int64
	if err := s.Scan(
		&iss.ID, &iss.Number, &iss.Title, &iss.Body, &iss.Author, &iss.State, &created, &updated,
	); err != nil {
		return iss, err
	}
	iss.CreatedAt = time.Unix(created, 0).UTC()
	iss.UpdatedAt = time.Unix(updated, 0).UTC()
	return iss, nil
}
