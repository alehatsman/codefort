package storage

import (
	"testing"

	"github.com/alehatsman/codefort/internal/api"
)

func TestChildProgress(t *testing.T) {
	db, repoID, nums := seedNIssues(t, 4)
	epic, c1, c2, c3 := nums[0], nums[1], nums[2], nums[3]
	for _, c := range []int{c1, c2, c3} {
		if _, err := UpdateIssue(db, repoID, c, nil, nil, nil, &epic, nil); err != nil {
			t.Fatalf("set parent %d->%d: %v", c, epic, err)
		}
	}
	// Mark two children done/closed.
	done, closed := api.IssueDone, api.IssueClosed
	if _, err := UpdateIssue(db, repoID, c1, &done, nil, nil, nil, nil); err != nil {
		t.Fatalf("c1 done: %v", err)
	}
	if _, err := UpdateIssue(db, repoID, c2, &closed, nil, nil, nil, nil); err != nil {
		t.Fatalf("c2 closed: %v", err)
	}

	prog, err := ChildProgress(db, repoID)
	if err != nil {
		t.Fatalf("ChildProgress: %v", err)
	}
	p, ok := prog[epic]
	if !ok {
		t.Fatalf("no progress for epic #%d", epic)
	}
	if p.Total != 3 || p.Done != 2 {
		t.Fatalf("progress = %d/%d, want 2/3", p.Done, p.Total)
	}
	// A non-epic leaf must be absent from the map.
	if _, ok := prog[c3]; ok {
		t.Fatalf("leaf #%d should have no progress entry", c3)
	}
}

func TestEpicsFilter(t *testing.T) {
	db, repoID, nums := seedNIssues(t, 3)
	epic, child, leaf := nums[0], nums[1], nums[2]
	if _, err := UpdateIssue(db, repoID, child, nil, nil, nil, &epic, nil); err != nil {
		t.Fatalf("set parent: %v", err)
	}
	out, err := ListIssues(db, repoID, ListFilter{Epics: true})
	if err != nil {
		t.Fatalf("ListIssues epics: %v", err)
	}
	got := issueNums(out)
	if !got[epic] {
		t.Errorf("#%d (epic) should be listed", epic)
	}
	if got[child] || got[leaf] {
		t.Errorf("epics view should be {#%d}, got %v", epic, got)
	}
}

func TestCountIssuesHonorsEpics(t *testing.T) {
	db, repoID, nums := seedNIssues(t, 3)
	epic, child := nums[0], nums[1]
	if _, err := UpdateIssue(db, repoID, child, nil, nil, nil, &epic, nil); err != nil {
		t.Fatalf("set parent: %v", err)
	}
	n, err := CountIssues(db, repoID, ListFilter{Epics: true})
	if err != nil {
		t.Fatalf("CountIssues: %v", err)
	}
	if n != 1 {
		t.Fatalf("count epics = %d, want 1", n)
	}
}
