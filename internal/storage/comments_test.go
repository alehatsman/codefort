package storage

import (
	"errors"
	"testing"

	"github.com/alehatsman/codefort/internal/api"
)

// DeleteComment is author-scoped and classifies misses: a non-author delete is
// forbidden, the author's delete succeeds, and a second delete (the row is now
// gone) reports not-found rather than a false success (#189).
func TestDeleteCommentAuthorOnlyAndMiss(t *testing.T) {
	db, repoID, number := seedIssue(t)
	iss, err := GetIssue(db, repoID, number)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	issueID := iss.ID

	c, err := CreateComment(db, issueID, api.CreateCommentRequest{Author: "alice", Body: "hi"})
	if err != nil {
		t.Fatalf("CreateComment: %v", err)
	}

	// A non-author can't delete it, and the row survives.
	if err := DeleteComment(db, c.ID, "bob"); !errors.Is(err, ErrForbidden) {
		t.Errorf("delete by non-author err = %v, want ErrForbidden", err)
	}
	got, err := ListComments(db, issueID)
	if err != nil || len(got) != 1 {
		t.Fatalf("after forbidden delete: comments = %v, err = %v; want 1 still present", got, err)
	}

	// The author deletes it.
	if err := DeleteComment(db, c.ID, "alice"); err != nil {
		t.Fatalf("delete by author: %v", err)
	}

	// Re-deleting the now-gone row is ErrNotFound, not a silent success — the
	// authorized DELETE matched no rows, so the miss path classifies it.
	if err := DeleteComment(db, c.ID, "alice"); !errors.Is(err, ErrNotFound) {
		t.Errorf("re-delete err = %v, want ErrNotFound", err)
	}

	// An id that never existed is also ErrNotFound.
	if err := DeleteComment(db, 99999, "alice"); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete nonexistent err = %v, want ErrNotFound", err)
	}
}
