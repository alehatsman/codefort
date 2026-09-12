package storage

import (
	"database/sql"
	"errors"
	"time"

	"github.com/alehatsman/codefort/internal/api"
)

// CreateComment appends a comment to an issue. issueID is the row id,
// not the per-repo number.
func CreateComment(db *sql.DB, issueID int64, req api.CreateCommentRequest) (api.Comment, error) {
	if req.Author == "" || req.Body == "" {
		return api.Comment{}, ErrInvalidInput
	}
	row := db.QueryRow(`
		INSERT INTO issue_comments(issue_id, author, body)
		VALUES (?, ?, ?)
		RETURNING id, issue_id, author, body, created_at
	`, issueID, req.Author, req.Body)
	return scanComment(row)
}

// ListComments returns comments for an issue in chronological order.
func ListComments(db *sql.DB, issueID int64) ([]api.Comment, error) {
	rows, err := db.Query(`
		SELECT id, issue_id, author, body, created_at
		FROM issue_comments
		WHERE issue_id = ?
		ORDER BY created_at ASC, id ASC
	`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	comments := make([]api.Comment, 0)
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		comments = append(comments, c)
	}
	return comments, rows.Err()
}

// DeleteComment removes a comment by id, but only if requester matches
// the comment's author. Returns ErrNotFound or ErrForbidden so the
// handler can pick the right HTTP status.
//
// The authorization rides on the DELETE itself (WHERE id = ? AND author = ?)
// rather than a separate read-then-delete, so there's no TOCTOU window and no
// false "deleted" when the row vanished between the two statements. A zero
// RowsAffected means the delete didn't happen; only then do we read the row to
// tell "not found" from "forbidden" (#189).
func DeleteComment(db *sql.DB, commentID int64, requester string) error {
	res, err := db.Exec(`DELETE FROM issue_comments WHERE id = ? AND author = ?`, commentID, requester)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return deleteMiss(db, `SELECT 1 FROM issue_comments WHERE id = ?`, commentID)
	}
	return nil
}

// deleteMiss classifies why an authorized DELETE matched no rows: the row never
// existed (ErrNotFound) or it exists but belongs to someone else (ErrForbidden).
// existsQuery must select any column for the row by its id. Run only on the
// (rare) miss path, so the happy path stays a single statement.
func deleteMiss(db *sql.DB, existsQuery string, id int64) error {
	var x int
	switch err := db.QueryRow(existsQuery, id).Scan(&x); {
	case errors.Is(err, sql.ErrNoRows):
		return ErrNotFound
	case err != nil:
		return err
	default:
		return ErrForbidden
	}
}

func scanComment(s scanner) (api.Comment, error) {
	var c api.Comment
	var created int64
	if err := s.Scan(&c.ID, &c.IssueID, &c.Author, &c.Body, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c, ErrNotFound
		}
		return c, err
	}
	c.CreatedAt = time.Unix(created, 0).UTC() // created_at stored as Unix seconds
	return c, nil
}
