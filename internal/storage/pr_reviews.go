package storage

import (
	"database/sql"
	"time"

	"github.com/alehatsman/codefort/internal/api"
)

// UpsertReview inserts or replaces the caller's review state for a PR.
func UpsertReview(db *sql.DB, repoID int64, prNumber int, author string, state api.PRReviewState) (api.PRReview, error) {
	_, err := db.Exec(`
		INSERT INTO pr_reviews (repo_id, pr_number, author, state, updated_at)
		VALUES (?, ?, ?, ?, strftime('%s','now'))
		ON CONFLICT (repo_id, pr_number, author)
		DO UPDATE SET state = excluded.state, updated_at = excluded.updated_at`,
		repoID, prNumber, author, state,
	)
	if err != nil {
		return api.PRReview{}, err
	}
	return GetReview(db, repoID, prNumber, author)
}

// GetReview returns a single review by (repo, pr, author).
func GetReview(db *sql.DB, repoID int64, prNumber int, author string) (api.PRReview, error) {
	var r api.PRReview
	var updatedAt int64
	err := db.QueryRow(`
		SELECT id, author, state, updated_at
		FROM pr_reviews WHERE repo_id=? AND pr_number=? AND author=?`,
		repoID, prNumber, author,
	).Scan(&r.ID, &r.Author, &r.State, &updatedAt)
	if err != nil {
		return api.PRReview{}, err
	}
	r.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	return r, nil
}

// ListReviews returns all reviews for a PR ordered by updated_at desc.
func ListReviews(db *sql.DB, repoID int64, prNumber int) ([]api.PRReview, error) {
	rows, err := db.Query(`
		SELECT id, author, state, updated_at
		FROM pr_reviews WHERE repo_id=? AND pr_number=?
		ORDER BY updated_at DESC`,
		repoID, prNumber,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []api.PRReview
	for rows.Next() {
		var r api.PRReview
		var updatedAt int64
		if err := rows.Scan(&r.ID, &r.Author, &r.State, &updatedAt); err != nil {
			return nil, err
		}
		r.UpdatedAt = time.Unix(updatedAt, 0).UTC()
		out = append(out, r)
	}
	return out, rows.Err()
}
