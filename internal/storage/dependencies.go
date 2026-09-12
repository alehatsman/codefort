package storage

import (
	"database/sql"
	"errors"

	"github.com/alehatsman/codefort/internal/api"
)

// ErrDependencyCycle is returned by AddDependency when the requested edge would
// introduce a cycle, breaking the DAG invariant the --ready/next derivations
// rely on. The handler maps it to 409 (conflict).
var ErrDependencyCycle = errors.New("dependency would create a cycle")

// ErrSelfDependency is returned when an issue is asked to depend on itself.
// Mapped to 400.
var ErrSelfDependency = errors.New("an issue cannot depend on itself")

// AddDependency records that issueNum depends on dependsOn (issueNum is blocked
// until dependsOn is done). Both numbers must reference existing issues in the
// repo. A self-edge is rejected (ErrSelfDependency); an edge that would close a
// cycle is rejected (ErrDependencyCycle). Adding an edge that already exists is
// an idempotent success. Returns ErrInvalidInput if either issue is missing.
func AddDependency(db *sql.DB, repoID int64, issueNum, dependsOn int) error {
	if issueNum == dependsOn {
		return ErrSelfDependency
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Both endpoints must exist in this repo.
	for _, n := range []int{issueNum, dependsOn} {
		var one int
		err := tx.QueryRow(`SELECT 1 FROM issues WHERE repo_id = ? AND number = ?`, repoID, n).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInvalidInput
		}
		if err != nil {
			return err
		}
	}

	// Cycle check: the edge issueNum -> dependsOn closes a cycle iff dependsOn
	// can already reach issueNum by following existing depends-on edges. Walk
	// the dependency graph forward from dependsOn; if we arrive back at
	// issueNum, the edge is illegal.
	reaches, err := dependsOnReaches(tx, repoID, dependsOn, issueNum)
	if err != nil {
		return err
	}
	if reaches {
		return ErrDependencyCycle
	}

	if _, err := tx.Exec(`
		INSERT OR IGNORE INTO issue_dependencies(repo_id, issue_number, depends_on_number)
		VALUES (?, ?, ?)
	`, repoID, issueNum, dependsOn); err != nil {
		return err
	}
	return tx.Commit()
}

// dependsOnReaches reports whether `from` can reach `target` by following
// depends-on edges (from depends on X, X depends on Y, ...). Used for cycle
// detection before inserting a new edge. Iterative DFS with a visited set so a
// pre-existing cycle (shouldn't happen, but be defensive) can't loop forever.
func dependsOnReaches(tx *sql.Tx, repoID int64, from, target int) (bool, error) {
	visited := map[int]bool{}
	stack := []int{from}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cur == target {
			return true, nil
		}
		if visited[cur] {
			continue
		}
		visited[cur] = true

		rows, err := tx.Query(
			`SELECT depends_on_number FROM issue_dependencies WHERE repo_id = ? AND issue_number = ?`,
			repoID, cur,
		)
		if err != nil {
			return false, err
		}
		var next []int
		for rows.Next() {
			var n int
			if err := rows.Scan(&n); err != nil {
				rows.Close()
				return false, err
			}
			next = append(next, n)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return false, err
		}
		rows.Close()
		stack = append(stack, next...)
	}
	return false, nil
}

// RemoveDependency deletes the issueNum -> dependsOn edge if present. Removing a
// non-existent edge is an idempotent success (the post-state is the same).
func RemoveDependency(db *sql.DB, repoID int64, issueNum, dependsOn int) error {
	_, err := db.Exec(
		`DELETE FROM issue_dependencies WHERE repo_id = ? AND issue_number = ? AND depends_on_number = ?`,
		repoID, issueNum, dependsOn,
	)
	return err
}

// ListDependencies returns the issues that issueNum depends on (its blockers),
// ordered by number ascending. These are the "blocked-by" edges.
func ListDependencies(db *sql.DB, repoID int64, issueNum int) ([]api.IssueRef, error) {
	return scanIssueRefs(db, `
		SELECT i.number, i.title, i.state
		  FROM issue_dependencies d
		  JOIN issues i ON i.repo_id = d.repo_id AND i.number = d.depends_on_number
		 WHERE d.repo_id = ? AND d.issue_number = ?
		 ORDER BY i.number ASC
	`, repoID, issueNum)
}

// ListDependents returns the issues that depend on issueNum (the issues it
// blocks), ordered by number ascending. These are the "blocks" edges.
func ListDependents(db *sql.DB, repoID int64, issueNum int) ([]api.IssueRef, error) {
	return scanIssueRefs(db, `
		SELECT i.number, i.title, i.state
		  FROM issue_dependencies d
		  JOIN issues i ON i.repo_id = d.repo_id AND i.number = d.issue_number
		 WHERE d.repo_id = ? AND d.depends_on_number = ?
		 ORDER BY i.number ASC
	`, repoID, issueNum)
}

// scanIssueRefs runs a query selecting (number, title, state) and collects the
// rows into IssueRefs. Shared by ListDependencies/ListDependents.
func scanIssueRefs(db *sql.DB, query string, args ...any) ([]api.IssueRef, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]api.IssueRef, 0)
	for rows.Next() {
		var r api.IssueRef
		if err := rows.Scan(&r.Number, &r.Title, &r.State); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
