package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/alehatsman/moongit/internal/api"
)

// ErrAlreadyClaimed is returned by Claim when the target issue already has
// an assignee. Distinct from ErrNotFound so the handler can map to 409.
var ErrAlreadyClaimed = errors.New("issue already claimed")

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

	// Validate parent: must exist in the same repo and not be self-referential
	// (self-ref can't happen on create since the issue doesn't exist yet, but
	// check for existence regardless).
	if req.Parent != nil && *req.Parent > 0 {
		var exists int
		if err := tx.QueryRow(
			`SELECT 1 FROM issues WHERE repo_id = ? AND number = ?`, repoID, *req.Parent,
		).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
			return api.Issue{}, ErrInvalidInput
		} else if err != nil {
			return api.Issue{}, err
		}
	}

	var parentArg any
	if req.Parent != nil && *req.Parent > 0 {
		parentArg = *req.Parent
	}

	labelsJSON, err := json.Marshal(labelsOrEmpty(req.Labels))
	if err != nil {
		return api.Issue{}, err
	}

	row := tx.QueryRow(`
		INSERT INTO issues(repo_id, number, title, body, author, state, parent_number, labels)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING `+issueColumns+`
	`, repoID, next, req.Title, req.Body, req.Author, string(api.IssueTodo), parentArg, string(labelsJSON))

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
	row := db.QueryRow(`SELECT `+issueColumns+` FROM issues WHERE repo_id = ? AND number = ?`, repoID, number)
	iss, err := scanIssue(row)
	if errors.Is(err, sql.ErrNoRows) {
		return iss, ErrNotFound
	}
	return iss, err
}

// DeleteIssue hard-deletes an issue and its comments. Comments are removed
// explicitly in the same transaction rather than relying on the FK ON DELETE
// CASCADE, so the behavior holds even if the foreign_keys pragma is ever off.
// Returns ErrNotFound if (repoID, number) doesn't exist. Repo issue counts
// are computed on read, so nothing else needs touching.
func DeleteIssue(db *sql.DB, repoID int64, number int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var id int64
	err = tx.QueryRow(`SELECT id FROM issues WHERE repo_id = ? AND number = ?`, repoID, number).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM issue_comments WHERE issue_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM issues WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// ErrNoUpdateFields is returned by UpdateIssue when every field is nil —
// there's nothing to change. The handler maps it to 400.
var ErrNoUpdateFields = errors.New("no fields to update")

// UpdateIssue applies a partial update: only the non-nil fields are written,
// and updated_at is bumped. Returns the updated row, ErrNotFound if
// (repoID, number) doesn't exist, or ErrNoUpdateFields if nothing was given.
// Validation (state values, non-empty title) is the caller's responsibility.
// parent: nil = no change, 0 = clear, >0 = set to that number (validated).
// labels: nil = no change; non-nil (even empty slice) = replace entire set.
func UpdateIssue(db *sql.DB, repoID int64, number int, state *api.IssueState, title, body *string, parent *int, labels *[]string) (api.Issue, error) {
	sets := []string{"updated_at = strftime('%s', 'now')"}
	args := []any{}
	if state != nil {
		sets = append(sets, "state = ?")
		args = append(args, string(*state))
	}
	if title != nil {
		sets = append(sets, "title = ?")
		args = append(args, *title)
	}
	if body != nil {
		sets = append(sets, "body = ?")
		args = append(args, *body)
	}
	if parent != nil {
		if *parent == 0 {
			sets = append(sets, "parent_number = NULL")
		} else {
			// Validate: parent must exist in same repo and not be self.
			var exists int
			err := db.QueryRow(
				`SELECT 1 FROM issues WHERE repo_id = ? AND number = ?`, repoID, *parent,
			).Scan(&exists)
			if errors.Is(err, sql.ErrNoRows) {
				return api.Issue{}, ErrInvalidInput
			}
			if err != nil {
				return api.Issue{}, err
			}
			if *parent == number {
				return api.Issue{}, ErrInvalidInput
			}
			sets = append(sets, "parent_number = ?")
			args = append(args, *parent)
		}
	}
	if labels != nil {
		b, err := json.Marshal(labelsOrEmpty(*labels))
		if err != nil {
			return api.Issue{}, err
		}
		sets = append(sets, "labels = ?")
		args = append(args, string(b))
	}
	// Only the bumped updated_at — caller passed no real fields.
	if len(sets) == 1 {
		return api.Issue{}, ErrNoUpdateFields
	}
	args = append(args, repoID, number)

	row := db.QueryRow(`
		UPDATE issues
		   SET `+strings.Join(sets, ", ")+`
		 WHERE repo_id = ? AND number = ?
		 RETURNING `+issueColumns+`
	`, args...)
	iss, err := scanIssue(row)
	if errors.Is(err, sql.ErrNoRows) {
		return iss, ErrNotFound
	}
	return iss, err
}

// Claim atomically takes ownership of an issue, stamping claimed_at as the
// lease start. It succeeds when the issue is unclaimed, when the same
// assignee re-claims (heartbeat — refreshes the lease), or when the existing
// claim is older than lease (orphaned by a crashed agent). A non-positive
// lease disables expiry: only unclaimed issues and heartbeats succeed.
// Returns ErrAlreadyClaimed if a live claim is held by someone else,
// ErrNotFound if the issue doesn't exist.
func Claim(db *sql.DB, repoID int64, number int, assignee string, state api.IssueState, lease time.Duration) (api.Issue, error) {
	// Compare-and-set in a single UPDATE — the WHERE clause is the lock.
	// If 0 rows change, distinguish "not found" from "live claim by other".
	tx, err := db.Begin()
	if err != nil {
		return api.Issue{}, err
	}
	defer tx.Rollback()

	var setState string
	args := []any{assignee}
	if state != "" {
		setState = ", state = ?"
		args = append(args, string(state))
	}
	// WHERE args: repo, number, heartbeat-owner, [lease seconds].
	args = append(args, repoID, number, assignee)
	expiry := ""
	if lease > 0 {
		expiry = " OR claimed_at <= strftime('%s','now') - ?"
		args = append(args, int64(lease.Seconds()))
	}

	res, err := tx.Exec(`
		UPDATE issues
		   SET assignee = ?, claimed_at = strftime('%s','now'), updated_at = strftime('%s','now')`+setState+`
		 WHERE repo_id = ? AND number = ?
		   AND (assignee IS NULL OR assignee = ?`+expiry+`)
	`, args...)
	if err != nil {
		return api.Issue{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return api.Issue{}, err
	}
	if n == 0 {
		// Distinguish missing-issue from a live claim held by someone else.
		row := tx.QueryRow(`SELECT 1 FROM issues WHERE repo_id = ? AND number = ?`, repoID, number)
		var one int
		if err := row.Scan(&one); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return api.Issue{}, ErrNotFound
			}
			return api.Issue{}, err
		}
		return api.Issue{}, ErrAlreadyClaimed
	}

	row := tx.QueryRow(`SELECT `+issueColumns+` FROM issues WHERE repo_id = ? AND number = ?`, repoID, number)
	iss, err := scanIssue(row)
	if err != nil {
		return api.Issue{}, err
	}
	return iss, tx.Commit()
}

// Unclaim clears the assignee and claim lease, but only for the current
// owner. Returns ErrNotFound if the issue doesn't exist, ErrNotOwner if a
// different agent holds the claim. Unclaiming an already-unclaimed issue is
// idempotent success (the result is the same regardless of who asks).
func Unclaim(db *sql.DB, repoID int64, number int, caller string) (api.Issue, error) {
	tx, err := db.Begin()
	if err != nil {
		return api.Issue{}, err
	}
	defer tx.Rollback()

	cur, err := scanIssue(tx.QueryRow(
		`SELECT `+issueColumns+` FROM issues WHERE repo_id = ? AND number = ?`, repoID, number))
	if errors.Is(err, sql.ErrNoRows) {
		return api.Issue{}, ErrNotFound
	}
	if err != nil {
		return api.Issue{}, err
	}
	// Already unclaimed — nothing to do, same outcome for any caller.
	if cur.Assignee == nil {
		return cur, tx.Commit()
	}
	if *cur.Assignee != caller {
		return api.Issue{}, ErrNotOwner
	}

	iss, err := scanIssue(tx.QueryRow(`
		UPDATE issues
		   SET assignee = NULL, claimed_at = NULL, updated_at = strftime('%s','now')
		 WHERE repo_id = ? AND number = ?
		 RETURNING `+issueColumns+`
	`, repoID, number))
	if err != nil {
		return api.Issue{}, err
	}
	return iss, tx.Commit()
}

// ExpireClaims releases every claim older than lease, clearing assignee and
// claimed_at so orphaned work (a crashed agent) becomes discoverable as
// unassigned rather than only stealable on the next competing claim. Returns
// the number of claims released. A non-positive lease is a no-op — expiry is
// disabled. State is intentionally left untouched; releasing ownership is the
// reaper's only job.
//
// Terminal states (done/closed) are skipped: their assignee is completion
// attribution ("who did it"), not a live lease, and must survive indefinitely.
func ExpireClaims(db *sql.DB, lease time.Duration) (int64, error) {
	if lease <= 0 {
		return 0, nil
	}
	res, err := db.Exec(`
		UPDATE issues
		   SET assignee = NULL, claimed_at = NULL, updated_at = strftime('%s','now')
		 WHERE assignee IS NOT NULL
		   AND state NOT IN ('done','closed')
		   AND claimed_at <= strftime('%s','now') - ?
	`, int64(lease.Seconds()))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ListFilter narrows the result set for ListIssues. Empty fields are
// ignored (no filter). Assignee == "null" matches unassigned issues
// specifically; an empty Assignee means "any."
type ListFilter struct {
	States   []api.IssueState // OR-match; nil/empty means any
	Assignee string           // "" = any, "null" = unassigned, otherwise exact
	Author   string           // "" = any, otherwise exact
	Query    string           // "" = any; case-insensitive substring of title or body
	Label    string           // "" = any; issue must have this label (exact, case-sensitive)
	Sort     api.IssueSort    // "" = default (newest); see api.IssueSort
	Limit    int              // 0 = default (100), capped at 1000
	Offset   int              // rows to skip (page * limit); <=0 = none
}

// likeEscape neutralizes the LIKE wildcards (% and _) and the escape char
// itself so a user's query matches literally as a substring rather than as a
// pattern. Paired with `ESCAPE '\'` in the query.
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func ListIssues(db *sql.DB, repoID int64, filter ListFilter) ([]api.Issue, error) {
	q := strings.Builder{}
	q.WriteString(`SELECT ` + issueColumns + ` FROM issues WHERE repo_id = ?`)
	args := []any{repoID}
	appendIssueFilters(&q, &args, filter)

	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	switch filter.Sort {
	case api.IssueSortOldest:
		q.WriteString(" ORDER BY number ASC")
	case api.IssueSortRecentlyUpdated:
		// number DESC tie-breaks issues sharing an updated_at (e.g. created in
		// the same instant) so the order is stable.
		q.WriteString(" ORDER BY updated_at DESC, number DESC")
	default: // IssueSortNewest and the unset zero value
		q.WriteString(" ORDER BY number DESC")
	}
	q.WriteString(" LIMIT ?")
	args = append(args, limit)
	if filter.Offset > 0 {
		q.WriteString(" OFFSET ?")
		args = append(args, filter.Offset)
	}

	rows, err := db.Query(q.String(), args...)
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

// issueColumns is the canonical select list, used everywhere so scanIssue
// stays in sync with INSERT/UPDATE RETURNING and SELECT.
const issueColumns = "id, number, title, body, author, state, assignee, claimed_at, parent_number, created_at, updated_at, labels"

// scanner abstracts *sql.Row and *sql.Rows so scanIssue can serve both.
type scanner interface {
	Scan(dest ...any) error
}

func scanIssue(s scanner) (api.Issue, error) {
	var iss api.Issue
	var assignee sql.NullString
	var claimed sql.NullInt64
	var parent sql.NullInt64
	var created, updated int64
	var labelsJSON string
	if err := s.Scan(
		&iss.ID, &iss.Number, &iss.Title, &iss.Body, &iss.Author, &iss.State,
		&assignee, &claimed, &parent, &created, &updated, &labelsJSON,
	); err != nil {
		return iss, err
	}
	decodeIssue(&iss, assignee, claimed, parent, created, updated, labelsJSON)
	return iss, nil
}

// decodeIssue fills an Issue's nullable/time/json fields from the raw column values,
// shared by scanIssue and the cross-repo aggregate scan so the two never drift.
func decodeIssue(iss *api.Issue, assignee sql.NullString, claimed sql.NullInt64, parent sql.NullInt64, created, updated int64, labelsJSON string) {
	if assignee.Valid {
		iss.Assignee = &assignee.String
	}
	if claimed.Valid {
		ts := time.Unix(claimed.Int64, 0).UTC()
		iss.ClaimedAt = &ts
	}
	if parent.Valid {
		n := int(parent.Int64)
		iss.ParentNumber = &n
	}
	iss.CreatedAt = time.Unix(created, 0).UTC()
	iss.UpdatedAt = time.Unix(updated, 0).UTC()
	if labelsJSON != "" {
		_ = json.Unmarshal([]byte(labelsJSON), &iss.Labels)
	}
	if iss.Labels == nil {
		iss.Labels = []string{}
	}
}

// labelsOrEmpty returns the slice as-is if non-nil, otherwise an empty slice,
// so JSON marshaling always produces [] rather than null.
func labelsOrEmpty(ls []string) []string {
	if ls == nil {
		return []string{}
	}
	return ls
}

// appendIssueFilters writes the shared state/assignee/author/query predicates
// onto an in-progress issue query, used by both ListIssues and ListAllIssues.
// Every referenced column is unique to the issues table, so the clauses stay
// correct unqualified even under the repos/users join the aggregate query adds.
func appendIssueFilters(q *strings.Builder, args *[]any, filter ListFilter) {
	if len(filter.States) > 0 {
		q.WriteString(" AND state IN (")
		for i, s := range filter.States {
			if i > 0 {
				q.WriteString(",")
			}
			q.WriteString("?")
			*args = append(*args, string(s))
		}
		q.WriteString(")")
	}
	if filter.Assignee == "null" {
		q.WriteString(" AND assignee IS NULL")
	} else if filter.Assignee != "" {
		q.WriteString(" AND assignee = ?")
		*args = append(*args, filter.Assignee)
	}
	if filter.Author != "" {
		q.WriteString(" AND author = ?")
		*args = append(*args, filter.Author)
	}
	if filter.Query != "" {
		// LIKE is case-insensitive for ASCII in SQLite by default, which is
		// fine for a keyword search. Match the same %term% against title and
		// body; wildcards in the term are escaped so they're taken literally.
		pat := "%" + likeEscape(filter.Query) + "%"
		q.WriteString(` AND (title LIKE ? ESCAPE '\' OR body LIKE ? ESCAPE '\')`)
		*args = append(*args, pat, pat)
	}
	if filter.Label != "" {
		// json_each expands the labels JSON array into rows; the EXISTS check
		// is true when at least one element equals the requested label exactly.
		q.WriteString(` AND EXISTS (SELECT 1 FROM json_each(issues.labels) WHERE value = ?)`)
		*args = append(*args, filter.Label)
	}
}

// ListAllIssues returns issues across every repo, newest-updated first, each
// tagged with its owning repo. It backs GET /api/issues (the fleet-wide Issues
// view). The state/assignee/author/query filters and limit cap behave as in
// ListIssues; Sort is ignored — per-repo issue numbers aren't globally
// orderable, so the cross-repo feed is always by recency (updated_at), with
// issues.id breaking ties for a stable order.
func ListAllIssues(db *sql.DB, filter ListFilter) ([]api.IssueWithRepo, error) {
	q := strings.Builder{}
	// Qualify with the issues. prefix: id/created_at also exist on repos/users
	// under the join, so a bare issueColumns would be ambiguous.
	q.WriteString(`SELECT issues.id, issues.number, issues.title, issues.body, issues.author, issues.state, issues.assignee, issues.claimed_at, issues.parent_number, issues.created_at, issues.updated_at, issues.labels, users.name, repos.name
		FROM issues
		JOIN repos ON repos.id = issues.repo_id
		JOIN users ON users.id = repos.owner_id
		WHERE 1=1`)
	args := []any{}
	appendIssueFilters(&q, &args, filter)

	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	q.WriteString(" ORDER BY issues.updated_at DESC, issues.id DESC LIMIT ?")
	args = append(args, limit)
	if filter.Offset > 0 {
		q.WriteString(" OFFSET ?")
		args = append(args, filter.Offset)
	}

	rows, err := db.Query(q.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]api.IssueWithRepo, 0)
	for rows.Next() {
		var iss api.Issue
		var assignee sql.NullString
		var claimed sql.NullInt64
		var parent sql.NullInt64
		var created, updated int64
		var labelsJSON string
		var owner, name string
		if err := rows.Scan(
			&iss.ID, &iss.Number, &iss.Title, &iss.Body, &iss.Author, &iss.State,
			&assignee, &claimed, &parent, &created, &updated, &labelsJSON, &owner, &name,
		); err != nil {
			return nil, err
		}
		decodeIssue(&iss, assignee, claimed, parent, created, updated, labelsJSON)
		out = append(out, api.IssueWithRepo{Issue: iss, Repo: api.RepoRef{Owner: owner, Name: name}})
	}
	return out, rows.Err()
}

// CountIssues returns how many issues in a repo match the filter's
// state/assignee/author/query predicates. Limit/Offset/Sort are ignored —
// it's the total for pagination, not a page. Mirrors ListIssues' WHERE.
func CountIssues(db *sql.DB, repoID int64, filter ListFilter) (int, error) {
	q := strings.Builder{}
	q.WriteString(`SELECT COUNT(*) FROM issues WHERE repo_id = ?`)
	args := []any{repoID}
	appendIssueFilters(&q, &args, filter)
	var n int
	err := db.QueryRow(q.String(), args...).Scan(&n)
	return n, err
}

// CountAllIssues is the cross-repo analogue of CountIssues, backing the
// X-Total-Count on GET /api/issues. Mirrors ListAllIssues' WHERE + join.
func CountAllIssues(db *sql.DB, filter ListFilter) (int, error) {
	q := strings.Builder{}
	q.WriteString(`SELECT COUNT(*)
		FROM issues
		JOIN repos ON repos.id = issues.repo_id
		JOIN users ON users.id = repos.owner_id
		WHERE 1=1`)
	args := []any{}
	appendIssueFilters(&q, &args, filter)
	var n int
	err := db.QueryRow(q.String(), args...).Scan(&n)
	return n, err
}

// ListChildren returns all direct children of the given issue (issues whose
// parent_number == number in the same repo), ordered by number ascending.
func ListChildren(db *sql.DB, repoID int64, number int) ([]api.ChildIssueSummary, error) {
	rows, err := db.Query(
		`SELECT number, title, state FROM issues WHERE repo_id = ? AND parent_number = ? ORDER BY number ASC`,
		repoID, number,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]api.ChildIssueSummary, 0)
	for rows.Next() {
		var c api.ChildIssueSummary
		if err := rows.Scan(&c.Number, &c.Title, &c.State); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
