package storage

import (
	"database/sql"
	"testing"

	"github.com/alehatsman/moongit/internal/api"
)

// seedTwoIssues creates two issues in the same repo and returns the db, repoID,
// and both issue numbers. parent is number 1, child candidate is number 2.
func seedTwoIssues(t *testing.T) (*sql.DB, int64, int, int) {
	t.Helper()
	db, repoID, num1 := seedIssue(t)
	req := api.CreateIssueRequest{Title: "child", Author: "alice"}
	iss2, err := CreateIssue(db, repoID, req)
	if err != nil {
		t.Fatalf("seedTwoIssues CreateIssue: %v", err)
	}
	return db, repoID, num1, iss2.Number
}

func TestCreateIssueWithParent(t *testing.T) {
	db, repoID, parent, _ := seedTwoIssues(t)
	req := api.CreateIssueRequest{Title: "child with parent", Author: "alice", Parent: &parent}
	iss, err := CreateIssue(db, repoID, req)
	if err != nil {
		t.Fatalf("CreateIssue with parent: %v", err)
	}
	if iss.ParentNumber == nil || *iss.ParentNumber != parent {
		t.Fatalf("ParentNumber = %v, want %d", iss.ParentNumber, parent)
	}
}

func TestCreateIssueWithInvalidParent(t *testing.T) {
	db, repoID, _, _ := seedTwoIssues(t)
	missing := 9999
	req := api.CreateIssueRequest{Title: "bad parent", Author: "alice", Parent: &missing}
	if _, err := CreateIssue(db, repoID, req); err != ErrInvalidInput {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
}

func TestUpdateIssueSetParent(t *testing.T) {
	db, repoID, parent, child := seedTwoIssues(t)
	iss, err := UpdateIssue(db, repoID, child, nil, nil, nil, &parent, nil)
	if err != nil {
		t.Fatalf("UpdateIssue set parent: %v", err)
	}
	if iss.ParentNumber == nil || *iss.ParentNumber != parent {
		t.Fatalf("ParentNumber = %v, want %d", iss.ParentNumber, parent)
	}
}

func TestUpdateIssueClearParent(t *testing.T) {
	db, repoID, parent, child := seedTwoIssues(t)
	if _, err := UpdateIssue(db, repoID, child, nil, nil, nil, &parent, nil); err != nil {
		t.Fatalf("set parent: %v", err)
	}
	zero := 0
	iss, err := UpdateIssue(db, repoID, child, nil, nil, nil, &zero, nil)
	if err != nil {
		t.Fatalf("clear parent: %v", err)
	}
	if iss.ParentNumber != nil {
		t.Fatalf("ParentNumber = %v, want nil after clear", iss.ParentNumber)
	}
}

func TestUpdateIssueSelfParentRejected(t *testing.T) {
	db, repoID, num, _ := seedTwoIssues(t)
	if _, err := UpdateIssue(db, repoID, num, nil, nil, nil, &num, nil); err != ErrInvalidInput {
		t.Fatalf("err = %v, want ErrInvalidInput for self-parent", err)
	}
}

func TestListChildren(t *testing.T) {
	db, repoID, parent, child := seedTwoIssues(t)
	if _, err := UpdateIssue(db, repoID, child, nil, nil, nil, &parent, nil); err != nil {
		t.Fatalf("set parent: %v", err)
	}
	children, err := ListChildren(db, repoID, parent)
	if err != nil {
		t.Fatalf("ListChildren: %v", err)
	}
	if len(children) != 1 {
		t.Fatalf("len(children) = %d, want 1", len(children))
	}
	if children[0].Number != child {
		t.Fatalf("child.Number = %d, want %d", children[0].Number, child)
	}
}

func TestListChildrenEmpty(t *testing.T) {
	db, repoID, parent, _ := seedTwoIssues(t)
	children, err := ListChildren(db, repoID, parent)
	if err != nil {
		t.Fatalf("ListChildren: %v", err)
	}
	if len(children) != 0 {
		t.Fatalf("len(children) = %d, want 0", len(children))
	}
}
