package storage

import (
	"database/sql"
	"testing"

	"github.com/alehatsman/moongit/internal/api"
)

// nums returns the issue numbers from a ListIssues result, for set comparisons.
func issueNums(issues []api.Issue) map[int]bool {
	m := map[int]bool{}
	for _, iss := range issues {
		m[iss.Number] = true
	}
	return m
}

func listReady(t *testing.T, db *sql.DB, repoID int64) []api.Issue {
	t.Helper()
	out, err := ListIssues(db, repoID, ListFilter{Ready: true})
	if err != nil {
		t.Fatalf("ListIssues ready: %v", err)
	}
	return out
}

func TestReadyExcludesBlockedEpicAndClaimed(t *testing.T) {
	db, repoID, nums := seedNIssues(t, 5)
	a, b, c, d, e := nums[0], nums[1], nums[2], nums[3], nums[4]

	// a: plain todo leaf — ready.
	// b: blocked by c (c still todo) — not ready, but blocked.
	if err := AddDependency(db, repoID, b, c); err != nil {
		t.Fatalf("b->c: %v", err)
	}
	// d: an epic (e is its child) — not ready (it's a map).
	if _, err := UpdateIssue(db, repoID, e, nil, nil, nil, &d, nil); err != nil {
		t.Fatalf("set parent e->d: %v", err)
	}
	// c: claimed by someone — not ready.
	if _, err := Claim(db, repoID, c, "agent-x", "", testLease); err != nil {
		t.Fatalf("claim c: %v", err)
	}

	ready := issueNums(listReady(t, db, repoID))
	// a is ready; e is a leaf child with no deps and unclaimed → ready too.
	if !ready[a] {
		t.Errorf("#%d (plain leaf) should be ready", a)
	}
	if !ready[e] {
		t.Errorf("#%d (leaf child, no deps) should be ready", e)
	}
	if ready[b] {
		t.Errorf("#%d (blocked) must not be ready", b)
	}
	if ready[c] {
		t.Errorf("#%d (claimed) must not be ready", c)
	}
	if ready[d] {
		t.Errorf("#%d (epic) must not be ready", d)
	}
}

func TestReadyBecomesReadyWhenDependencyDone(t *testing.T) {
	db, repoID, nums := seedNIssues(t, 2)
	a, b := nums[0], nums[1]
	if err := AddDependency(db, repoID, a, b); err != nil {
		t.Fatalf("a->b: %v", err)
	}
	if issueNums(listReady(t, db, repoID))[a] {
		t.Fatalf("#%d must not be ready while #%d is todo", a, b)
	}
	// Close the blocker; a should now surface as ready.
	done := api.IssueDone
	if _, err := UpdateIssue(db, repoID, b, &done, nil, nil, nil, nil); err != nil {
		t.Fatalf("mark b done: %v", err)
	}
	if !issueNums(listReady(t, db, repoID))[a] {
		t.Fatalf("#%d should be ready once #%d is done", a, b)
	}
}

func TestBlockedShowsUnmetLeavesOnly(t *testing.T) {
	db, repoID, nums := seedNIssues(t, 3)
	a, b, c := nums[0], nums[1], nums[2]
	// a depends on b (unmet) → blocked. c is a plain leaf → not blocked.
	if err := AddDependency(db, repoID, a, b); err != nil {
		t.Fatalf("a->b: %v", err)
	}
	out, err := ListIssues(db, repoID, ListFilter{Blocked: true})
	if err != nil {
		t.Fatalf("ListIssues blocked: %v", err)
	}
	got := issueNums(out)
	if !got[a] {
		t.Errorf("#%d should be blocked", a)
	}
	if got[b] || got[c] {
		t.Errorf("blocked set should be {#%d}, got %v", a, got)
	}
}

func TestReadyIgnoredOnAggregate(t *testing.T) {
	// The cross-repo aggregate does not honor Ready/Blocked; assert the flag is
	// simply ignored there rather than erroring.
	db, _, _ := seedNIssues(t, 1)
	if _, err := ListAllIssues(db, ListFilter{Ready: true}); err != nil {
		t.Fatalf("ListAllIssues with Ready: %v", err)
	}
}
