package storage

import (
	"database/sql"
	"errors"
)

var (
	ErrNotFound     = errors.New("not found")
	ErrForbidden    = errors.New("forbidden")
	ErrInvalidInput = errors.New("invalid input")
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
	// CIStatus is the status of the repo's most recent CI-kind run (highest run
	// number among kind='ci'), empty when the repo has no CI runs. Agent runs
	// share the ci_runs table but are excluded so the icon tracks pipeline
	// automation only. CINumber is that run's per-repo number, for linking to
	// it. Surfaced so the repos list can show an at-a-glance CI icon without a
	// per-repo follow-up query.
	CIStatus string
	CINumber int
	// OpenPulls is the count of open pull requests (state='open'). OpenReviews
	// is the count of unresolved code-review comments (resolved=0). ActiveAgents
	// is the count of agent runs in a non-terminal state (queued/running/
	// awaiting_input/finishing). All three are computed by correlated subqueries
	// so the repos list can show the per-repo metric grid without follow-up
	// queries.
	OpenPulls    int
	OpenReviews  int
	ActiveAgents int
	Visibility   string
}

// repoMetricSubqueries are the three correlated counts shared by ListRepos and
// GetRepoSummary, appended after the CI columns. Kept as one constant so both
// queries — and the column order in scanRepoSummary — stay in lockstep.
const repoMetricSubqueries = `
	  (SELECT COUNT(*) FROM pull_requests p WHERE p.repo_id = repos.id AND p.state = 'open') AS open_pulls,
	  (SELECT COUNT(*) FROM code_comments cc WHERE cc.repo_id = repos.id AND cc.resolved = 0) AS open_reviews,
	  (SELECT COUNT(*) FROM ci_runs ar WHERE ar.repo_id = repos.id AND ar.kind = 'agent' AND ar.status IN ('queued','running','awaiting_input','finishing')) AS active_agents`

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
		  repos.visibility,
		  (SELECT cr.status FROM ci_runs cr WHERE cr.repo_id = repos.id AND cr.kind = 'ci' ORDER BY cr.number DESC LIMIT 1) AS ci_status,
		  (SELECT cr.number FROM ci_runs cr WHERE cr.repo_id = repos.id AND cr.kind = 'ci' ORDER BY cr.number DESC LIMIT 1) AS ci_number,` + repoMetricSubqueries + `
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
		  repos.visibility,
		  (SELECT cr.status FROM ci_runs cr WHERE cr.repo_id = repos.id AND cr.kind = 'ci' ORDER BY cr.number DESC LIMIT 1) AS ci_status,
		  (SELECT cr.number FROM ci_runs cr WHERE cr.repo_id = repos.id AND cr.kind = 'ci' ORDER BY cr.number DESC LIMIT 1) AS ci_number,`+repoMetricSubqueries+`
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
		&r.Visibility,
		&ciStatus, &ciNumber,
		&r.OpenPulls, &r.OpenReviews, &r.ActiveAgents,
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

// DeleteRepo hard-deletes a repo by id. Every child table references repos(id)
// ON DELETE CASCADE — issues (and their comments, transitively), ci_runs,
// code_comments, pull_requests, and events — so a single DELETE removes the
// whole tree. The connection pool opens every handle with foreign_keys(1) (see
// Open), so the cascade is always in effect. Returns ErrNotFound when no repo
// with that id exists. The caller removes the on-disk bare git dir afterwards.
func DeleteRepo(db *sql.DB, repoID int64) error {
	res, err := db.Exec(`DELETE FROM repos WHERE id = ?`, repoID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
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

// SetRepoVisibility sets a repo's visibility to "public" or "private".
// Returns ErrNotFound when no repo with that id exists.
func SetRepoVisibility(db *sql.DB, repoID int64, visibility string) error {
	if visibility != "public" && visibility != "private" {
		return ErrInvalidInput
	}
	res, err := db.Exec(`UPDATE repos SET visibility = ? WHERE id = ?`, visibility, repoID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListReposForCaller returns repos visible to the given caller (by token name).
// Public repos are always visible. Private repos are only visible when the
// caller is the owner or a member. callerName="" is treated as an anonymous
// caller that can only see public repos.
func ListReposForCaller(db *sql.DB, callerName string) ([]RepoSummary, error) {
	rows, err := db.Query(`
		SELECT
		  repos.id,
		  users.name AS owner,
		  repos.name AS name,
		  repos.created_at,
		  COALESCE(SUM(CASE WHEN issues.state IN ('todo','in_progress') THEN 1 ELSE 0 END), 0) AS open_issues,
		  COALESCE(COUNT(issues.id), 0) AS total_issues,
		  repos.ci_enabled,
		  repos.visibility,
		  (SELECT cr.status FROM ci_runs cr WHERE cr.repo_id = repos.id AND cr.kind = 'ci' ORDER BY cr.number DESC LIMIT 1) AS ci_status,
		  (SELECT cr.number FROM ci_runs cr WHERE cr.repo_id = repos.id AND cr.kind = 'ci' ORDER BY cr.number DESC LIMIT 1) AS ci_number,`+repoMetricSubqueries+`
		FROM repos
		JOIN users ON users.id = repos.owner_id
		LEFT JOIN issues ON issues.repo_id = repos.id
		WHERE repos.visibility = 'public'
		   OR users.name = ?
		   OR EXISTS (
		        SELECT 1 FROM repo_members rm
		        JOIN users mu ON mu.id = rm.user_id
		        WHERE rm.repo_id = repos.id AND mu.name = ?
		      )
		GROUP BY repos.id
		ORDER BY users.name ASC, repos.name ASC
	`, callerName, callerName)
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
