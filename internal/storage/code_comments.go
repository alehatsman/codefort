package storage

import (
	"database/sql"
	"errors"
	"time"

	"github.com/alehatsman/codefort/internal/api"
)

// CreateCodeComment anchors a new comment to a file line range on a branch.
// repoID is the row id. Author and CommitSha are stamped by the caller.
func CreateCodeComment(db *sql.DB, repoID int64, req api.CreateCodeCommentRequest) (api.CodeComment, error) {
	row := db.QueryRow(`
		INSERT INTO code_comments(repo_id, ref, path, start_line, end_line, commit_sha, author, body)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id, repo_id, ref, path, start_line, end_line, commit_sha, author, body, resolved, created_at
	`, repoID, req.Ref, req.Path, req.StartLine, req.EndLine, req.CommitSha, req.Author, req.Body)
	return scanCodeComment(row)
}

// ListCodeComments returns a repo's code comments on a branch, oldest first.
// An empty path lists the whole branch; a non-empty path scopes to one file.
// Resolved comments are included only when includeResolved is set.
func ListCodeComments(db *sql.DB, repoID int64, ref, path string, includeResolved bool) ([]api.CodeComment, error) {
	q := `
		SELECT id, repo_id, ref, path, start_line, end_line, commit_sha, author, body, resolved, created_at
		FROM code_comments
		WHERE repo_id = ? AND ref = ?`
	args := []any{repoID, ref}
	if path != "" {
		q += ` AND path = ?`
		args = append(args, path)
	}
	if !includeResolved {
		q += ` AND resolved = 0`
	}
	q += ` ORDER BY path ASC, start_line ASC, created_at ASC, id ASC`

	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	comments := make([]api.CodeComment, 0)
	for rows.Next() {
		c, err := scanCodeComment(rows)
		if err != nil {
			return nil, err
		}
		comments = append(comments, c)
	}
	return comments, rows.Err()
}

// SetCodeCommentResolved flips a comment's resolved flag, but only if requester
// matches the author. Returns ErrNotFound or ErrForbidden so the handler can
// pick the right HTTP status.
func SetCodeCommentResolved(db *sql.DB, id int64, resolved bool, requester string) (api.CodeComment, error) {
	// Fold the author check into the write (one authorized UPDATE) so it shares
	// the hardened, race-free pattern DeleteCodeComment uses (#189). On no row,
	// deleteMiss distinguishes a missing comment (ErrNotFound) from one owned by
	// someone else (ErrForbidden).
	c, err := scanCodeComment(db.QueryRow(`
		UPDATE code_comments SET resolved = ? WHERE id = ? AND author = ?
		RETURNING id, repo_id, ref, path, start_line, end_line, commit_sha, author, body, resolved, created_at
	`, resolved, id, requester))
	if errors.Is(err, ErrNotFound) {
		return api.CodeComment{}, deleteMiss(db, `SELECT 1 FROM code_comments WHERE id = ?`, id)
	}
	return c, err
}

// DeleteCodeComment removes a comment by id, but only if requester matches the
// author. Returns ErrNotFound or ErrForbidden, mirroring DeleteComment — including
// the single authorized DELETE that avoids the read-then-delete TOCTOU (#189).
func DeleteCodeComment(db *sql.DB, id int64, requester string) error {
	res, err := db.Exec(`DELETE FROM code_comments WHERE id = ? AND author = ?`, id, requester)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return deleteMiss(db, `SELECT 1 FROM code_comments WHERE id = ?`, id)
	}
	return nil
}

func scanCodeComment(s scanner) (api.CodeComment, error) {
	var c api.CodeComment
	var created int64
	if err := s.Scan(
		&c.ID, &c.RepoID, &c.Ref, &c.Path, &c.StartLine, &c.EndLine,
		&c.CommitSha, &c.Author, &c.Body, &c.Resolved, &created,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c, ErrNotFound
		}
		return c, err
	}
	c.CreatedAt = time.Unix(created, 0).UTC()
	return c, nil
}
