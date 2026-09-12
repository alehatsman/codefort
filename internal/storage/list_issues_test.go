package storage

import (
	"database/sql"
	"slices"
	"testing"

	"github.com/alehatsman/moongit/internal/api"
)

// seedRepo (a migrated temp DB + one repo, no issues) lives in ci_test.go.

func mustCreate(t *testing.T, db *sql.DB, repoID int64, title, body string) {
	t.Helper()
	if _, err := CreateIssue(db, repoID, api.CreateIssueRequest{
		Title: title, Body: body, Author: "alice",
	}); err != nil {
		t.Fatalf("CreateIssue %q: %v", title, err)
	}
}

func mustCreateAs(t *testing.T, db *sql.DB, repoID int64, title, author string) {
	t.Helper()
	if _, err := CreateIssue(db, repoID, api.CreateIssueRequest{
		Title: title, Author: author,
	}); err != nil {
		t.Fatalf("CreateIssue %q: %v", title, err)
	}
}

func numbers(issues []api.Issue) map[int]bool {
	m := make(map[int]bool, len(issues))
	for _, iss := range issues {
		m[iss.Number] = true
	}
	return m
}

// order lists the result's issue numbers in result order — for asserting
// ListIssues' ORDER BY, where order is the thing under test (unlike numbers,
// which compares as a set).
func order(issues []api.Issue) []int {
	out := make([]int, len(issues))
	for i, iss := range issues {
		out[i] = iss.Number
	}
	return out
}

func TestListIssuesQueryMatchesTitleAndBody(t *testing.T) {
	db, repoID := seedRepo(t)
	mustCreate(t, db, repoID, "Fix the parser", "")        // #1: title match
	mustCreate(t, db, repoID, "Unrelated", "parser tweak") // #2: body match
	mustCreate(t, db, repoID, "Nothing here", "nope")      // #3: no match

	got, err := ListIssues(db, repoID, ListFilter{Query: "parser"})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	have := numbers(got)
	if !have[1] || !have[2] {
		t.Fatalf("query 'parser' = %v, want issues #1 and #2", have)
	}
	if have[3] {
		t.Fatalf("query 'parser' matched #3, which has no 'parser'")
	}
}

func TestListIssuesQueryIsCaseInsensitive(t *testing.T) {
	db, repoID := seedRepo(t)
	mustCreate(t, db, repoID, "Refactor Storage Layer", "")

	got, err := ListIssues(db, repoID, ListFilter{Query: "STORAGE"})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("case-insensitive query = %d issues, want 1", len(got))
	}
}

func TestListIssuesQueryCombinesWithStateFilter(t *testing.T) {
	db, repoID := seedRepo(t)
	mustCreate(t, db, repoID, "deploy script", "") // #1 stays todo
	mustCreate(t, db, repoID, "deploy docs", "")   // #2 -> done
	done := api.IssueDone
	if _, err := UpdateIssue(db, repoID, 2, &done, nil, nil, nil, nil); err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}

	got, err := ListIssues(db, repoID, ListFilter{
		Query:  "deploy",
		States: []api.IssueState{api.IssueTodo},
	})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(got) != 1 || got[0].Number != 1 {
		t.Fatalf("query+state = %v, want only #1 (todo)", numbers(got))
	}
}

// A LIKE wildcard in the query must be matched literally, not as a pattern,
// so "%" doesn't silently match every issue.
func TestListIssuesQueryEscapesWildcards(t *testing.T) {
	db, repoID := seedRepo(t)
	mustCreate(t, db, repoID, "plain title", "")
	mustCreate(t, db, repoID, "100% done", "")

	got, err := ListIssues(db, repoID, ListFilter{Query: "%"})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(got) != 1 || got[0].Number != 2 {
		t.Fatalf("query '%%' = %v, want only the issue literally containing '%%'", numbers(got))
	}
}

func TestListIssuesFiltersByAuthor(t *testing.T) {
	db, repoID := seedRepo(t)
	mustCreateAs(t, db, repoID, "alice one", "alice") // #1
	mustCreateAs(t, db, repoID, "bob one", "bob")     // #2
	mustCreateAs(t, db, repoID, "alice two", "alice") // #3

	got, err := ListIssues(db, repoID, ListFilter{Author: "alice"})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	have := numbers(got)
	if len(got) != 2 || !have[1] || !have[3] {
		t.Fatalf("author 'alice' = %v, want #1 and #3", have)
	}
}

func TestListIssuesSort(t *testing.T) {
	db, repoID := seedRepo(t)
	mustCreate(t, db, repoID, "first", "")  // #1
	mustCreate(t, db, repoID, "second", "") // #2
	mustCreate(t, db, repoID, "third", "")  // #3
	// updated_at is unix-seconds, so same-second creation would tie. Set
	// explicit, distinct timestamps to make the recency order deterministic:
	// #1 newest, then #3, then #2.
	for num, ts := range map[int]int64{1: 3000, 3: 2000, 2: 1000} {
		if _, err := db.Exec("UPDATE issues SET updated_at = ? WHERE repo_id = ? AND number = ?", ts, repoID, num); err != nil {
			t.Fatalf("set updated_at: %v", err)
		}
	}

	cases := []struct {
		name string
		sort api.IssueSort
		want []int
	}{
		{"default is newest (number desc)", "", []int{3, 2, 1}},
		{"newest", api.IssueSortNewest, []int{3, 2, 1}},
		{"oldest", api.IssueSortOldest, []int{1, 2, 3}},
		{"recently-updated", api.IssueSortRecentlyUpdated, []int{1, 3, 2}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ListIssues(db, repoID, ListFilter{Sort: tc.sort})
			if err != nil {
				t.Fatalf("ListIssues: %v", err)
			}
			if g := order(got); !slices.Equal(g, tc.want) {
				t.Errorf("sort %q = %v, want %v", tc.sort, g, tc.want)
			}
		})
	}
}

func TestListIssuesOffsetPaginates(t *testing.T) {
	db, repoID := seedRepo(t)
	for i := 1; i <= 5; i++ {
		mustCreate(t, db, repoID, "issue", "")
	}
	// Default sort is number DESC: #5,#4,#3,#2,#1. Page size 2.
	page1, err := ListIssues(db, repoID, ListFilter{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if g := order(page1); !slices.Equal(g, []int{5, 4}) {
		t.Fatalf("page1 = %v, want [5 4]", g)
	}
	page2, err := ListIssues(db, repoID, ListFilter{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if g := order(page2); !slices.Equal(g, []int{3, 2}) {
		t.Fatalf("page2 = %v, want [3 2]", g)
	}
	page3, err := ListIssues(db, repoID, ListFilter{Limit: 2, Offset: 4})
	if err != nil {
		t.Fatalf("page3: %v", err)
	}
	if g := order(page3); !slices.Equal(g, []int{1}) {
		t.Fatalf("page3 = %v, want [1]", g)
	}
}

func TestCountIssuesIgnoresLimitOffset(t *testing.T) {
	db, repoID := seedRepo(t)
	mustCreate(t, db, repoID, "Fix the parser", "")
	mustCreate(t, db, repoID, "Unrelated", "")
	mustCreate(t, db, repoID, "parser again", "")

	// Total ignores Limit/Offset — it's the full matching set.
	n, err := CountIssues(db, repoID, ListFilter{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("CountIssues: %v", err)
	}
	if n != 3 {
		t.Fatalf("count = %d, want 3", n)
	}
	// Filters still apply: only the two 'parser' rows.
	n, err = CountIssues(db, repoID, ListFilter{Query: "parser"})
	if err != nil {
		t.Fatalf("CountIssues filtered: %v", err)
	}
	if n != 2 {
		t.Fatalf("filtered count = %d, want 2", n)
	}
}

// Multi-word queries AND their terms across title and body, in any order — the
// old single-substring match required the words be adjacent and in order.
func TestListIssuesQueryAndsItsTerms(t *testing.T) {
	db, repoID := seedRepo(t)
	mustCreate(t, db, repoID, "Branch protection", "")             // #1: both, reordered
	mustCreate(t, db, repoID, "Protect the release", "branch tip") // #2: split across fields
	mustCreate(t, db, repoID, "Protect the tag", "")               // #3: only one term

	got, err := ListIssues(db, repoID, ListFilter{Query: "protect branch"})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	have := numbers(got)
	if !have[1] || !have[2] {
		t.Fatalf("query 'protect branch' = %v, want #1 and #2", have)
	}
	if have[3] {
		t.Fatalf("query 'protect branch' matched #3, which lacks 'branch'")
	}
}

// Terms past the cap are dropped, not rejected — the search narrows as far as
// the server ranks and still returns something usable.
func TestListIssuesQueryCapsTermCount(t *testing.T) {
	db, repoID := seedRepo(t)
	mustCreate(t, db, repoID, "a b c d e f g h", "")

	got, err := ListIssues(db, repoID, ListFilter{Query: "a b c d e f g h zzz"})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	// 'zzz' is the 9th term and appears nowhere; dropping it is what lets the
	// issue match at all.
	if len(got) != 1 {
		t.Fatalf("over-cap query = %d issues, want 1 (surplus term dropped)", len(got))
	}
}

// A title hit outranks a body hit, ahead of the newest-first default. #2 is the
// newer issue, so plain newest-first would put it first.
func TestListIssuesRanksTitleHitsFirst(t *testing.T) {
	db, repoID := seedRepo(t)
	mustCreate(t, db, repoID, "Rewrite the scheduler", "") // #1: title hit
	mustCreate(t, db, repoID, "Unrelated work", "the scheduler is slow")

	got, err := ListIssues(db, repoID, ListFilter{Query: "scheduler"})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if want := []int{1, 2}; !slices.Equal(order(got), want) {
		t.Fatalf("ranked order = %v, want %v (title hit first)", order(got), want)
	}
}

// An explicit sort is the caller's word on ordering; relevance must not
// override it.
func TestListIssuesExplicitSortBeatsRelevance(t *testing.T) {
	db, repoID := seedRepo(t)
	mustCreate(t, db, repoID, "Rewrite the scheduler", "") // #1: title hit
	mustCreate(t, db, repoID, "Unrelated work", "the scheduler is slow")

	got, err := ListIssues(db, repoID, ListFilter{Query: "scheduler", Sort: api.IssueSortNewest})
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if want := []int{2, 1}; !slices.Equal(order(got), want) {
		t.Fatalf("sort=newest order = %v, want %v (unranked)", order(got), want)
	}
}
