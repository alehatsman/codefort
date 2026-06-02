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
const pullColumns = "id, number, base_ref, head_ref, title, body, author, state, created_at, updated_at, merged_at, merge_base_sha, merge_head_sha"

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
// the given states (OR-match; nil/empty means any) and to a case-insensitive
// keyword matched against title or body (query == "" means any).
func ListPulls(db *sql.DB, repoID int64, states []api.PRState, query string) ([]api.PullRequest, error) {
	q := strings.Builder{}
	q.WriteString(`SELECT ` + pullColumns + ` FROM pull_requests WHERE repo_id = ?`)
	args := []any{repoID}
	appendPullFilters(&q, &args, "", states, query)
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

// appendPullFilters writes the shared state/query predicates onto an in-progress
// pull-request query, used by both ListPulls and ListAllPulls. prefix qualifies
// the columns ("" for the single-repo query, "pull_requests." under the
// aggregate query's join where bare names would be ambiguous).
func appendPullFilters(q *strings.Builder, args *[]any, prefix string, states []api.PRState, query string) {
	if len(states) > 0 {
		q.WriteString(" AND " + prefix + "state IN (")
		for i, s := range states {
			if i > 0 {
				q.WriteString(",")
			}
			q.WriteString("?")
			*args = append(*args, string(s))
		}
		q.WriteString(")")
	}
	if query != "" {
		// LIKE is case-insensitive for ASCII in SQLite by default. Match the same
		// %term% against title and body; wildcards in the term are escaped so
		// they're taken literally. Mirrors appendIssueFilters' query clause.
		pat := "%" + likeEscape(query) + "%"
		q.WriteString(` AND (` + prefix + `title LIKE ? ESCAPE '\' OR ` + prefix + `body LIKE ? ESCAPE '\')`)
		*args = append(*args, pat, pat)
	}
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
		// Keep merged_at consistent with state in the same write. Moving away
		// from merged also clears the frozen compare SHAs (set by MarkMerged) so
		// a reopened PR computes its compare from the live refs again.
		if *state == api.PRMerged {
			sets = append(sets, "merged_at = strftime('%s', 'now')")
		} else {
			sets = append(sets, "merged_at = NULL", "merge_base_sha = NULL", "merge_head_sha = NULL")
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

// MarkMerged stamps a PR merged in one write: state=merged, merged_at=now, and
// the base/head branch tips frozen at merge time (baseSHA/headSHA) so the
// detail endpoint can reproduce the pre-merge compare. The merge endpoint is
// the only caller — UpdatePull rejects the merged transition. Returns
// ErrNotFound if (repoID, number) doesn't exist.
func MarkMerged(db *sql.DB, repoID int64, number int, baseSHA, headSHA string) (api.PullRequest, error) {
	row := db.QueryRow(`
		UPDATE pull_requests
		   SET state = ?,
		       updated_at = strftime('%s', 'now'),
		       merged_at = strftime('%s', 'now'),
		       merge_base_sha = ?,
		       merge_head_sha = ?
		 WHERE repo_id = ? AND number = ?
		 RETURNING `+pullColumns+`
	`, string(api.PRMerged), baseSHA, headSHA, repoID, number)
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
	var mergeBase, mergeHead sql.NullString
	var created, updated int64
	if err := s.Scan(
		&pr.ID, &pr.Number, &pr.BaseRef, &pr.HeadRef, &pr.Title, &body, &pr.Author, &pr.State,
		&created, &updated, &merged, &mergeBase, &mergeHead,
	); err != nil {
		return pr, err
	}
	decodePull(&pr, body, merged, mergeBase, mergeHead, created, updated)
	return pr, nil
}

// decodePull fills a PullRequest's nullable/time fields from the raw column
// values, shared by scanPull and the cross-repo aggregate scan.
func decodePull(pr *api.PullRequest, body sql.NullString, merged sql.NullInt64, mergeBase, mergeHead sql.NullString, created, updated int64) {
	pr.Body = body.String
	pr.CreatedAt = time.Unix(created, 0).UTC()
	pr.UpdatedAt = time.Unix(updated, 0).UTC()
	if merged.Valid {
		ts := time.Unix(merged.Int64, 0).UTC()
		pr.MergedAt = &ts
	}
	pr.MergeBaseSHA = mergeBase.String
	pr.MergeHeadSHA = mergeHead.String
}

// ListAllPulls returns pull requests across every repo, newest-updated first,
// each tagged with its owning repo. It backs GET /api/pulls. states filters by
// OR-match (nil/empty = any) and query is a case-insensitive title/body keyword
// (== "" means any), both as in ListPulls; ordering is by recency (updated_at)
// since per-repo PR numbers aren't globally orderable.
func ListAllPulls(db *sql.DB, states []api.PRState, query string) ([]api.PullRequestWithRepo, error) {
	q := strings.Builder{}
	// Qualify with pull_requests.: id/created_at/updated_at also exist on the
	// joined repos/users tables, so a bare pullColumns would be ambiguous.
	q.WriteString(`SELECT pull_requests.id, pull_requests.number, pull_requests.base_ref, pull_requests.head_ref, pull_requests.title, pull_requests.body, pull_requests.author, pull_requests.state, pull_requests.created_at, pull_requests.updated_at, pull_requests.merged_at, pull_requests.merge_base_sha, pull_requests.merge_head_sha, users.name, repos.name
		FROM pull_requests
		JOIN repos ON repos.id = pull_requests.repo_id
		JOIN users ON users.id = repos.owner_id
		WHERE 1=1`)
	args := []any{}
	appendPullFilters(&q, &args, "pull_requests.", states, query)
	q.WriteString(" ORDER BY pull_requests.updated_at DESC, pull_requests.id DESC")

	rows, err := db.Query(q.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]api.PullRequestWithRepo, 0)
	for rows.Next() {
		var pr api.PullRequest
		var body sql.NullString
		var merged sql.NullInt64
		var mergeBase, mergeHead sql.NullString
		var created, updated int64
		var owner, name string
		if err := rows.Scan(
			&pr.ID, &pr.Number, &pr.BaseRef, &pr.HeadRef, &pr.Title, &body, &pr.Author, &pr.State,
			&created, &updated, &merged, &mergeBase, &mergeHead, &owner, &name,
		); err != nil {
			return nil, err
		}
		decodePull(&pr, body, merged, mergeBase, mergeHead, created, updated)
		out = append(out, api.PullRequestWithRepo{PullRequest: pr, Repo: api.RepoRef{Owner: owner, Name: name}})
	}
	return out, rows.Err()
}
