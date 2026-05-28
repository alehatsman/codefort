package storage

import (
	"database/sql"
	"errors"
)

var ErrNotFound = errors.New("not found")

// EnsureUser returns the user id, creating the row if absent.
func EnsureUser(db *sql.DB, name string) (int64, error) {
	if _, err := db.Exec("INSERT OR IGNORE INTO users(name) VALUES (?)", name); err != nil {
		return 0, err
	}
	var id int64
	if err := db.QueryRow("SELECT id FROM users WHERE name = ?", name).Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

// EnsureRepo returns the repo id, creating both the owner user and the repo
// row if absent. Idempotent.
func EnsureRepo(db *sql.DB, owner, name string) (int64, error) {
	userID, err := EnsureUser(db, owner)
	if err != nil {
		return 0, err
	}
	if _, err := db.Exec("INSERT OR IGNORE INTO repos(owner_id, name) VALUES (?, ?)", userID, name); err != nil {
		return 0, err
	}
	var id int64
	if err := db.QueryRow("SELECT id FROM repos WHERE owner_id = ? AND name = ?", userID, name).Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

// RepoSummary is the shape returned by ListRepos / GetRepo for UI views.
// Counts are computed at query time; small N for personal use.
type RepoSummary struct {
	ID         int64
	Owner      string
	Name       string
	CreatedAt  int64
	OpenIssues int
	TotalIssues int
}

// ListRepos returns every registered repo with issue counts, alphabetically
// by owner/name. Intended for the repos list view.
func ListRepos(db *sql.DB) ([]RepoSummary, error) {
	rows, err := db.Query(`
		SELECT
		  repos.id,
		  users.name AS owner,
		  repos.name AS name,
		  repos.created_at,
		  COALESCE(SUM(CASE WHEN issues.state IN ('todo','in_progress') THEN 1 ELSE 0 END), 0) AS open_issues,
		  COALESCE(COUNT(issues.id), 0) AS total_issues
		FROM repos
		JOIN users ON users.id = repos.owner_id
		LEFT JOIN issues ON issues.repo_id = repos.id
		GROUP BY repos.id
		ORDER BY users.name ASC, repos.name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]RepoSummary, 0)
	for rows.Next() {
		var r RepoSummary
		if err := rows.Scan(&r.ID, &r.Owner, &r.Name, &r.CreatedAt, &r.OpenIssues, &r.TotalIssues); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetRepoSummary returns a single repo's summary or ErrNotFound.
func GetRepoSummary(db *sql.DB, owner, name string) (RepoSummary, error) {
	var r RepoSummary
	err := db.QueryRow(`
		SELECT
		  repos.id,
		  users.name AS owner,
		  repos.name AS name,
		  repos.created_at,
		  COALESCE(SUM(CASE WHEN issues.state IN ('todo','in_progress') THEN 1 ELSE 0 END), 0) AS open_issues,
		  COALESCE(COUNT(issues.id), 0) AS total_issues
		FROM repos
		JOIN users ON users.id = repos.owner_id
		LEFT JOIN issues ON issues.repo_id = repos.id
		WHERE users.name = ? AND repos.name = ?
		GROUP BY repos.id
	`, owner, name).Scan(&r.ID, &r.Owner, &r.Name, &r.CreatedAt, &r.OpenIssues, &r.TotalIssues)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

// LookupRepo returns the repo id or ErrNotFound when the owner/name pair is
// unknown.
func LookupRepo(db *sql.DB, owner, name string) (int64, error) {
	var id int64
	err := db.QueryRow(`
		SELECT repos.id FROM repos
		JOIN users ON users.id = repos.owner_id
		WHERE users.name = ? AND repos.name = ?
	`, owner, name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}
