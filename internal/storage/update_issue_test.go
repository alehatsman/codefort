package storage

import (
	"errors"
	"testing"

	"github.com/alehatsman/moongit/internal/api"
)

func TestUpdateIssueTitleOnlyLeavesBody(t *testing.T) {
	db, repoID, num := seedIssue(t)
	// Give the issue a body so we can prove a title-only edit preserves it.
	body := "original body"
	if _, err := UpdateIssue(db, repoID, num, nil, nil, &body); err != nil {
		t.Fatalf("seed body: %v", err)
	}

	title := "new title"
	iss, err := UpdateIssue(db, repoID, num, nil, &title, nil)
	if err != nil {
		t.Fatalf("UpdateIssue title: %v", err)
	}
	if iss.Title != "new title" {
		t.Fatalf("title = %q, want %q", iss.Title, "new title")
	}
	if iss.Body != "original body" {
		t.Fatalf("body = %q, want it preserved", iss.Body)
	}
}

func TestUpdateIssueBodyOnlyLeavesTitleAndState(t *testing.T) {
	db, repoID, num := seedIssue(t)
	done := api.IssueDone
	if _, err := UpdateIssue(db, repoID, num, &done, nil, nil); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	body := "just body"
	iss, err := UpdateIssue(db, repoID, num, nil, nil, &body)
	if err != nil {
		t.Fatalf("UpdateIssue body: %v", err)
	}
	if iss.Body != "just body" {
		t.Fatalf("body = %q, want %q", iss.Body, "just body")
	}
	if iss.State != api.IssueDone {
		t.Fatalf("state = %q, want it preserved as done", iss.State)
	}
	if iss.Title != "t" {
		t.Fatalf("title = %q, want it preserved", iss.Title)
	}
}

func TestUpdateIssueNoFieldsRejected(t *testing.T) {
	db, repoID, num := seedIssue(t)
	if _, err := UpdateIssue(db, repoID, num, nil, nil, nil); !errors.Is(err, ErrNoUpdateFields) {
		t.Fatalf("err = %v, want ErrNoUpdateFields", err)
	}
}

func TestUpdateIssueNotFound(t *testing.T) {
	db, repoID, _ := seedIssue(t)
	title := "x"
	if _, err := UpdateIssue(db, repoID, 9999, nil, &title, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
