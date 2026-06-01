package storage

import (
	"errors"
	"testing"

	"github.com/alehatsman/moongit/internal/api"
)

// Deleting a repo must cascade to every child row — issues (and their
// comments), ci_runs, code_comments, pull_requests, and events — leaving no
// orphans behind. The connection pool opens with foreign_keys(1), so the FK
// cascade does the work; this guards that the schema's ON DELETE CASCADE is
// actually wired up across the tables.
func TestDeleteRepoCascadesChildren(t *testing.T) {
	db, repoID := seedRepo(t)

	// An issue with a comment.
	iss, err := CreateIssue(db, repoID, api.CreateIssueRequest{Author: "alice", Title: "t", Body: "b"})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if _, err := CreateComment(db, iss.ID, api.CreateCommentRequest{Author: "alice", Body: "c"}); err != nil {
		t.Fatalf("CreateComment: %v", err)
	}

	// A CI run.
	if _, err := EnqueueRun(db, repoID, NewRun{CommitSHA: "a", Ref: "refs/heads/main", Event: "push"}); err != nil {
		t.Fatalf("EnqueueRun: %v", err)
	}

	// A repo-scoped event.
	if _, err := AppendEvent(db, "issue.created", repoID, "alice", "{}"); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	if err := DeleteRepo(db, repoID); err != nil {
		t.Fatalf("DeleteRepo: %v", err)
	}

	// The repo itself is gone.
	if _, err := LookupRepo(db, "alice", "repo"); !errors.Is(err, ErrNotFound) {
		t.Errorf("LookupRepo after delete = %v, want ErrNotFound", err)
	}

	// Every child table is empty for this repo.
	assertNoRows := func(label, query string, args ...any) {
		t.Helper()
		var n int
		if err := db.QueryRow(query, args...).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", label, err)
		}
		if n != 0 {
			t.Errorf("%s rows after delete = %d, want 0", label, n)
		}
	}
	assertNoRows("issues", `SELECT COUNT(*) FROM issues WHERE repo_id = ?`, repoID)
	assertNoRows("issue_comments", `SELECT COUNT(*) FROM issue_comments WHERE issue_id = ?`, iss.ID)
	assertNoRows("ci_runs", `SELECT COUNT(*) FROM ci_runs WHERE repo_id = ?`, repoID)
	assertNoRows("events", `SELECT COUNT(*) FROM events WHERE repo_id = ?`, repoID)
}

func TestDeleteRepoNotFound(t *testing.T) {
	db, _ := seedRepo(t)
	if err := DeleteRepo(db, 9999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteRepo(missing) = %v, want ErrNotFound", err)
	}
}
