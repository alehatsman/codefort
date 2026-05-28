package storage

import (
	"database/sql"
	"errors"
	"time"

	"github.com/alehatsman/moongit/internal/api"
)

// CreateComment appends a comment to an issue. issueID is the row id,
// not the per-repo number.
func CreateComment(db *sql.DB, issueID int64, req api.CreateCommentRequest) (api.Comment, error) {
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
func DeleteComment(db *sql.DB, commentID int64, requester string) error {
	var author string
	err := db.QueryRow(`SELECT author FROM issue_comments WHERE id = ?`, commentID).Scan(&author)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if author != requester {
		return ErrForbidden
	}
	if _, err := db.Exec(`DELETE FROM issue_comments WHERE id = ?`, commentID); err != nil {
		return err
	}
	return nil
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
	c.CreatedAt = time.Unix(created, 0).UTC()
	return c, nil
}
