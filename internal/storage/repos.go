package storage

import (
	"database/sql"
	"errors"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrForbidden = errors.New("forbidden")
	// ErrNotOwner is returned by Unclaim when the caller is not the current
	// assignee. Distinct from ErrForbidden so the issue handler can map it
	// to its own 403 message without coupling to comment semantics.
	ErrNotOwner = errors.New("not the issue owner")
)

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
	ID          int64
	Owner       string
	Name        string
	CreatedAt   int64
	OpenIssues  int
	TotalIssues int
	CIEnabled   bool
	// CIStatus is the status of the repo's most recent CI run (highest run
	// number), empty when the repo has no runs. CINumber is that run's
	// per-repo number, for linking to it. Surfaced so the repos list can show
	// an at-a-glance CI icon without a per-repo follow-up query.
	CIStatus string
	CINumber int
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
		  COALESCE(COUNT(issues.id), 0) AS total_issues,
		  repos.ci_enabled,
		  (SELECT cr.status FROM ci_runs cr WHERE cr.repo_id = repos.id ORDER BY cr.number DESC LIMIT 1) AS ci_status,
		  (SELECT cr.number FROM ci_runs cr WHERE cr.repo_id = repos.id ORDER BY cr.number DESC LIMIT 1) AS ci_number
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
		if err := scanRepoSummary(rows, &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetRepoSummary returns a single repo's summary or ErrNotFound.
func GetRepoSummary(db *sql.DB, owner, name string) (RepoSummary, error) {
	var r RepoSummary
	row := db.QueryRow(`
		SELECT
		  repos.id,
		  users.name AS owner,
		  repos.name AS name,
		  repos.created_at,
		  COALESCE(SUM(CASE WHEN issues.state IN ('todo','in_progress') THEN 1 ELSE 0 END), 0) AS open_issues,
		  COALESCE(COUNT(issues.id), 0) AS total_issues,
		  repos.ci_enabled,
		  (SELECT cr.status FROM ci_runs cr WHERE cr.repo_id = repos.id ORDER BY cr.number DESC LIMIT 1) AS ci_status,
		  (SELECT cr.number FROM ci_runs cr WHERE cr.repo_id = repos.id ORDER BY cr.number DESC LIMIT 1) AS ci_number
		FROM repos
		JOIN users ON users.id = repos.owner_id
		LEFT JOIN issues ON issues.repo_id = repos.id
		WHERE users.name = ? AND repos.name = ?
		GROUP BY repos.id
	`, owner, name)
	if err := scanRepoSummary(row, &r); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return r, ErrNotFound
		}
		return r, err
	}
	return r, nil
}

// scanRepoSummary scans one repos-list row into r. The CI columns are nullable
// (a repo with no runs yields NULL), so they're read through Null* and only
// copied across when present, leaving CIStatus/CINumber zero-valued otherwise.
func scanRepoSummary(s scanner, r *RepoSummary) error {
	var ciStatus sql.NullString
	var ciNumber sql.NullInt64
	if err := s.Scan(
		&r.ID, &r.Owner, &r.Name, &r.CreatedAt, &r.OpenIssues, &r.TotalIssues, &r.CIEnabled,
		&ciStatus, &ciNumber,
	); err != nil {
		return err
	}
	r.CIStatus = ciStatus.String
	r.CINumber = int(ciNumber.Int64)
	return nil
}

// RepoIdent returns a repo's owner and name by id, or ErrNotFound. The CI
// runner needs it to resolve the bare repo path and the on-disk event-log
// path from a run's repo_id.
func RepoIdent(db *sql.DB, repoID int64) (owner, name string, err error) {
	err = db.QueryRow(`
		SELECT users.name, repos.name FROM repos
		JOIN users ON users.id = repos.owner_id
		WHERE repos.id = ?
	`, repoID).Scan(&owner, &name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrNotFound
	}
	return owner, name, err
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
