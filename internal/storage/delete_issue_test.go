package storage

import (
	"errors"
	"testing"

	"github.com/alehatsman/codefort/internal/api"
)

func TestDeleteIssueRemovesIssueAndComments(t *testing.T) {
	db, repoID, num := seedIssue(t)

	// Resolve the issue row id so we can add (and later assert away) comments.
	iss, err := GetIssue(db, repoID, num)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	for _, body := range []string{"first", "second"} {
		if _, err := CreateComment(db, iss.ID, api.CreateCommentRequest{Author: "alice", Body: body}); err != nil {
			t.Fatalf("CreateComment: %v", err)
		}
	}

	if err := DeleteIssue(db, repoID, num); err != nil {
		t.Fatalf("DeleteIssue: %v", err)
	}

	if _, err := GetIssue(db, repoID, num); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetIssue after delete err = %v, want ErrNotFound", err)
	}

	comments, err := ListComments(db, iss.ID)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(comments) != 0 {
		t.Fatalf("comments after delete = %d, want 0", len(comments))
	}
}

func TestDeleteIssueNotFound(t *testing.T) {
	db, repoID, _ := seedIssue(t)
	if err := DeleteIssue(db, repoID, 9999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
