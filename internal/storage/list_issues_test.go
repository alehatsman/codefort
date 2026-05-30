package storage

import (
	"database/sql"
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

func numbers(issues []api.Issue) map[int]bool {
	m := make(map[int]bool, len(issues))
	for _, iss := range issues {
		m[iss.Number] = true
	}
	return m
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
	if _, err := UpdateIssue(db, repoID, 2, &done, nil, nil); err != nil {
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
