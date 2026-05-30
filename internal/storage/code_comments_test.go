package storage

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/alehatsman/moongit/internal/api"
)

// seedCodeComment spins up a migrated temp DB with one repo, returning the
// writer handle and the repo id.
func seedCodeRepo(t *testing.T) (db *sql.DB, repoID int64) {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "cc.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := Migrate(d); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	id, err := EnsureRepo(d, "alice", "repo")
	if err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}
	return d, id
}

func mkReq(ref, path string, start, end int, author, body string) api.CreateCodeCommentRequest {
	return api.CreateCodeCommentRequest{
		Ref: ref, Path: path, StartLine: start, EndLine: end, Author: author, Body: body, CommitSha: "deadbeef",
	}
}

func TestCreateAndListCodeComments(t *testing.T) {
	db, repoID := seedCodeRepo(t)

	if _, err := CreateCodeComment(db, repoID, mkReq("main", "a.go", 10, 12, "alice", "first")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := CreateCodeComment(db, repoID, mkReq("main", "b.go", 3, 3, "bob", "second")); err != nil {
		t.Fatalf("create: %v", err)
	}
	// A comment on a different branch must not leak into the main listing.
	if _, err := CreateCodeComment(db, repoID, mkReq("feature", "a.go", 1, 1, "alice", "other branch")); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := ListCodeComments(db, repoID, "main", "", false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d comments, want 2", len(got))
	}
	// Ordered by path then start line: a.go then b.go.
	if got[0].Path != "a.go" || got[0].StartLine != 10 || got[0].EndLine != 12 {
		t.Errorf("comment[0] = %+v", got[0])
	}
	if got[0].CommitSha != "deadbeef" || got[0].Author != "alice" || got[0].Resolved {
		t.Errorf("comment[0] fields = %+v", got[0])
	}
	if got[1].Path != "b.go" {
		t.Errorf("comment[1].Path = %q, want b.go", got[1].Path)
	}

	// Path scoping.
	scoped, err := ListCodeComments(db, repoID, "main", "a.go", false)
	if err != nil {
		t.Fatalf("list scoped: %v", err)
	}
	if len(scoped) != 1 || scoped[0].Path != "a.go" {
		t.Fatalf("scoped = %+v", scoped)
	}
}

func TestCodeCommentResolveLifecycle(t *testing.T) {
	db, repoID := seedCodeRepo(t)
	c, err := CreateCodeComment(db, repoID, mkReq("main", "a.go", 1, 2, "alice", "x"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Open list excludes nothing yet.
	if open, _ := ListCodeComments(db, repoID, "main", "", false); len(open) != 1 {
		t.Fatalf("open before resolve = %d, want 1", len(open))
	}

	// Author resolves it.
	updated, err := SetCodeCommentResolved(db, c.ID, true, "alice")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !updated.Resolved {
		t.Errorf("Resolved = false after resolve")
	}

	// Now excluded from the open list, present in the all list.
	if open, _ := ListCodeComments(db, repoID, "main", "", false); len(open) != 0 {
		t.Errorf("open after resolve = %d, want 0", len(open))
	}
	if all, _ := ListCodeComments(db, repoID, "main", "", true); len(all) != 1 {
		t.Errorf("all after resolve = %d, want 1", len(all))
	}

	// Re-open.
	if _, err := SetCodeCommentResolved(db, c.ID, false, "alice"); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if open, _ := ListCodeComments(db, repoID, "main", "", false); len(open) != 1 {
		t.Errorf("open after reopen = %d, want 1", len(open))
	}
}

func TestCodeCommentAuthorOnly(t *testing.T) {
	db, repoID := seedCodeRepo(t)
	c, err := CreateCodeComment(db, repoID, mkReq("main", "a.go", 1, 2, "alice", "x"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := SetCodeCommentResolved(db, c.ID, true, "bob"); !errors.Is(err, ErrForbidden) {
		t.Errorf("resolve by non-author err = %v, want ErrForbidden", err)
	}
	if err := DeleteCodeComment(db, c.ID, "bob"); !errors.Is(err, ErrForbidden) {
		t.Errorf("delete by non-author err = %v, want ErrForbidden", err)
	}

	// Author deletes.
	if err := DeleteCodeComment(db, c.ID, "alice"); err != nil {
		t.Fatalf("delete by author: %v", err)
	}
	if err := DeleteCodeComment(db, c.ID, "alice"); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete missing err = %v, want ErrNotFound", err)
	}
}
