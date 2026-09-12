package storage

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/alehatsman/codefort/internal/api"
)

// seedNIssues creates n issues in one repo and returns the db, repoID, and the
// slice of issue numbers (in creation order).
func seedNIssues(t *testing.T, n int) (db *sql.DB, repoID int64, nums []int) {
	t.Helper()
	d, id, num1 := seedIssue(t)
	nums = []int{num1}
	for i := 1; i < n; i++ {
		iss, err := CreateIssue(d, id, api.CreateIssueRequest{Title: "iss", Author: "alice"})
		if err != nil {
			t.Fatalf("seedNIssues CreateIssue: %v", err)
		}
		nums = append(nums, iss.Number)
	}
	return d, id, nums
}

func TestAddDependency(t *testing.T) {
	db, repoID, nums := seedNIssues(t, 2)
	a, b := nums[0], nums[1]
	if err := AddDependency(db, repoID, a, b); err != nil {
		t.Fatalf("AddDependency: %v", err)
	}
	deps, err := ListDependencies(db, repoID, a)
	if err != nil {
		t.Fatalf("ListDependencies: %v", err)
	}
	if len(deps) != 1 || deps[0].Number != b {
		t.Fatalf("deps = %+v, want [#%d]", deps, b)
	}
	// b should report a as a dependent (b blocks a).
	blocks, err := ListDependents(db, repoID, b)
	if err != nil {
		t.Fatalf("ListDependents: %v", err)
	}
	if len(blocks) != 1 || blocks[0].Number != a {
		t.Fatalf("blocks = %+v, want [#%d]", blocks, a)
	}
}

func TestAddDependencyIdempotent(t *testing.T) {
	db, repoID, nums := seedNIssues(t, 2)
	a, b := nums[0], nums[1]
	if err := AddDependency(db, repoID, a, b); err != nil {
		t.Fatalf("first AddDependency: %v", err)
	}
	if err := AddDependency(db, repoID, a, b); err != nil {
		t.Fatalf("second AddDependency: %v", err)
	}
	deps, _ := ListDependencies(db, repoID, a)
	if len(deps) != 1 {
		t.Fatalf("len(deps) = %d after duplicate add, want 1", len(deps))
	}
}

func TestAddDependencySelfRejected(t *testing.T) {
	db, repoID, nums := seedNIssues(t, 1)
	a := nums[0]
	if err := AddDependency(db, repoID, a, a); !errors.Is(err, ErrSelfDependency) {
		t.Fatalf("err = %v, want ErrSelfDependency", err)
	}
}

func TestAddDependencyMissingIssue(t *testing.T) {
	db, repoID, nums := seedNIssues(t, 1)
	a := nums[0]
	if err := AddDependency(db, repoID, a, 9999); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
	if err := AddDependency(db, repoID, 9999, a); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
}

func TestAddDependencyDirectCycleRejected(t *testing.T) {
	db, repoID, nums := seedNIssues(t, 2)
	a, b := nums[0], nums[1]
	if err := AddDependency(db, repoID, a, b); err != nil {
		t.Fatalf("a->b: %v", err)
	}
	// b->a would close a 2-cycle.
	if err := AddDependency(db, repoID, b, a); !errors.Is(err, ErrDependencyCycle) {
		t.Fatalf("err = %v, want ErrDependencyCycle", err)
	}
}

func TestAddDependencyTransitiveCycleRejected(t *testing.T) {
	db, repoID, nums := seedNIssues(t, 3)
	a, b, c := nums[0], nums[1], nums[2]
	// a->b->c, then c->a closes a 3-cycle.
	if err := AddDependency(db, repoID, a, b); err != nil {
		t.Fatalf("a->b: %v", err)
	}
	if err := AddDependency(db, repoID, b, c); err != nil {
		t.Fatalf("b->c: %v", err)
	}
	if err := AddDependency(db, repoID, c, a); !errors.Is(err, ErrDependencyCycle) {
		t.Fatalf("err = %v, want ErrDependencyCycle", err)
	}
}

func TestAddDependencyDiamondAllowed(t *testing.T) {
	// A diamond (a->b, a->c, b->d, c->d) is a DAG, not a cycle.
	db, repoID, nums := seedNIssues(t, 4)
	a, b, c, d := nums[0], nums[1], nums[2], nums[3]
	for _, e := range [][2]int{{a, b}, {a, c}, {b, d}, {c, d}} {
		if err := AddDependency(db, repoID, e[0], e[1]); err != nil {
			t.Fatalf("edge %d->%d: %v", e[0], e[1], err)
		}
	}
}

func TestRemoveDependency(t *testing.T) {
	db, repoID, nums := seedNIssues(t, 2)
	a, b := nums[0], nums[1]
	if err := AddDependency(db, repoID, a, b); err != nil {
		t.Fatalf("AddDependency: %v", err)
	}
	if err := RemoveDependency(db, repoID, a, b); err != nil {
		t.Fatalf("RemoveDependency: %v", err)
	}
	deps, _ := ListDependencies(db, repoID, a)
	if len(deps) != 0 {
		t.Fatalf("len(deps) = %d after remove, want 0", len(deps))
	}
	// Removing a non-existent edge is a no-op success.
	if err := RemoveDependency(db, repoID, a, b); err != nil {
		t.Fatalf("idempotent RemoveDependency: %v", err)
	}
}

func TestDeleteIssueClearsDependencies(t *testing.T) {
	db, repoID, nums := seedNIssues(t, 3)
	a, b, c := nums[0], nums[1], nums[2]
	// b depends on a; c depends on b. Deleting b must drop both edges.
	if err := AddDependency(db, repoID, b, a); err != nil {
		t.Fatalf("b->a: %v", err)
	}
	if err := AddDependency(db, repoID, c, b); err != nil {
		t.Fatalf("c->b: %v", err)
	}
	if err := DeleteIssue(db, repoID, b); err != nil {
		t.Fatalf("DeleteIssue: %v", err)
	}
	// a should no longer be blocking anything; c should have no blockers.
	if blocks, _ := ListDependents(db, repoID, a); len(blocks) != 0 {
		t.Fatalf("a still blocks %+v after deleting b", blocks)
	}
	if deps, _ := ListDependencies(db, repoID, c); len(deps) != 0 {
		t.Fatalf("c still depends on %+v after deleting b", deps)
	}
}
