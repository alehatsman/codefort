package storage

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/alehatsman/moongit/internal/api"
)

// pullColumns is the canonical select list, used everywhere so scanPull stays
// in sync with INSERT/UPDATE RETURNING and SELECT.
const pullColumns = "id, number, base_ref, head_ref, title, body, author, state, created_at, updated_at, merged_at"

// CreatePull allocates the next per-repo PR number and inserts the row. The
// (repo_id, number) UNIQUE constraint + SQLite's single-writer guarantee keep
// numbering monotonic without explicit locking — same pattern as CreateIssue.
// base/head are taken from req.Base/req.Head; validation that they name real
// branches is the caller's job.
func CreatePull(db *sql.DB, repoID int64, req api.CreatePullRequest) (api.PullRequest, error) {
	tx, err := db.Begin()
	if err != nil {
		return api.PullRequest{}, err
	}
	defer tx.Rollback()

	var next int
	if err := tx.QueryRow(
		"SELECT COALESCE(MAX(number), 0) + 1 FROM pull_requests WHERE repo_id = ?", repoID,
	).Scan(&next); err != nil {
		return api.PullRequest{}, err
	}

	row := tx.QueryRow(`
		INSERT INTO pull_requests(repo_id, number, base_ref, head_ref, title, body, author, state)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING `+pullColumns+`
	`, repoID, next, req.Base, req.Head, req.Title, req.Body, req.Author, string(api.PROpen))

	pr, err := scanPull(row)
	if err != nil {
		return api.PullRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return api.PullRequest{}, err
	}
	return pr, nil
}

func GetPull(db *sql.DB, repoID int64, number int) (api.PullRequest, error) {
	row := db.QueryRow(`SELECT `+pullColumns+` FROM pull_requests WHERE repo_id = ? AND number = ?`, repoID, number)
	pr, err := scanPull(row)
	if errors.Is(err, sql.ErrNoRows) {
		return pr, ErrNotFound
	}
	return pr, err
}

// ListPulls returns a repo's PRs, newest number first, optionally filtered to
// the given states (OR-match; nil/empty means any).
func ListPulls(db *sql.DB, repoID int64, states []api.PRState) ([]api.PullRequest, error) {
	q := strings.Builder{}
	q.WriteString(`SELECT ` + pullColumns + ` FROM pull_requests WHERE repo_id = ?`)
	args := []any{repoID}
	if len(states) > 0 {
		q.WriteString(" AND state IN (")
		for i, s := range states {
			if i > 0 {
				q.WriteString(",")
			}
			q.WriteString("?")
			args = append(args, string(s))
		}
		q.WriteString(")")
	}
	q.WriteString(" ORDER BY number DESC")

	rows, err := db.Query(q.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	pulls := make([]api.PullRequest, 0)
	for rows.Next() {
		pr, err := scanPull(rows)
		if err != nil {
			return nil, err
		}
		pulls = append(pulls, pr)
	}
	return pulls, rows.Err()
}

// UpdatePull applies a partial update: only the non-nil fields are written and
// updated_at is bumped. Moving state to "merged" stamps merged_at; moving away
// from "merged" clears it, so the invariant (merged_at non-NULL iff merged)
// always holds. Returns ErrNotFound if (repoID, number) doesn't exist, or
// ErrNoUpdateFields if nothing was given. Validation is the caller's job.
func UpdatePull(db *sql.DB, repoID int64, number int, title, body *string, state *api.PRState) (api.PullRequest, error) {
	sets := []string{"updated_at = strftime('%s', 'now')"}
	args := []any{}
	if title != nil {
		sets = append(sets, "title = ?")
		args = append(args, *title)
	}
	if body != nil {
		sets = append(sets, "body = ?")
		args = append(args, *body)
	}
	if state != nil {
		sets = append(sets, "state = ?")
		args = append(args, string(*state))
		// Keep merged_at consistent with state in the same write.
		if *state == api.PRMerged {
			sets = append(sets, "merged_at = strftime('%s', 'now')")
		} else {
			sets = append(sets, "merged_at = NULL")
		}
	}
	if len(sets) == 1 {
		return api.PullRequest{}, ErrNoUpdateFields
	}
	args = append(args, repoID, number)

	row := db.QueryRow(`
		UPDATE pull_requests
		   SET `+strings.Join(sets, ", ")+`
		 WHERE repo_id = ? AND number = ?
		 RETURNING `+pullColumns+`
	`, args...)
	pr, err := scanPull(row)
	if errors.Is(err, sql.ErrNoRows) {
		return pr, ErrNotFound
	}
	return pr, err
}

func scanPull(s scanner) (api.PullRequest, error) {
	var pr api.PullRequest
	var body sql.NullString
	var merged sql.NullInt64
	var created, updated int64
	if err := s.Scan(
		&pr.ID, &pr.Number, &pr.BaseRef, &pr.HeadRef, &pr.Title, &body, &pr.Author, &pr.State,
		&created, &updated, &merged,
	); err != nil {
		return pr, err
	}
	pr.Body = body.String
	pr.CreatedAt = time.Unix(created, 0).UTC()
	pr.UpdatedAt = time.Unix(updated, 0).UTC()
	if merged.Valid {
		ts := time.Unix(merged.Int64, 0).UTC()
		pr.MergedAt = &ts
	}
	return pr, nil
}
