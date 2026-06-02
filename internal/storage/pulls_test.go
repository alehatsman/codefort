package storage

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/alehatsman/moongit/internal/api"
)

func mustCreatePull(t *testing.T, db *sql.DB, repoID int64, base, head, title string) api.PullRequest {
	t.Helper()
	pr, err := CreatePull(db, repoID, api.CreatePullRequest{
		Base: base, Head: head, Title: title, Author: "alice",
	})
	if err != nil {
		t.Fatalf("CreatePull %q: %v", title, err)
	}
	return pr
}

func TestCreatePullAllocatesPerRepoNumbers(t *testing.T) {
	db, repoID := seedRepo(t)
	p1 := mustCreatePull(t, db, repoID, "main", "feature-1", "first")
	p2 := mustCreatePull(t, db, repoID, "main", "feature-2", "second")
	if p1.Number != 1 || p2.Number != 2 {
		t.Fatalf("numbers = %d, %d, want 1, 2", p1.Number, p2.Number)
	}
	if p1.State != api.PROpen {
		t.Errorf("new PR state = %q, want open", p1.State)
	}
	if p1.MergedAt != nil {
		t.Errorf("new PR merged_at = %v, want nil", p1.MergedAt)
	}
	if p1.BaseRef != "main" || p1.HeadRef != "feature-1" {
		t.Errorf("refs = %q/%q, want main/feature-1", p1.BaseRef, p1.HeadRef)
	}
}

func TestGetPullNotFound(t *testing.T) {
	db, repoID := seedRepo(t)
	if _, err := GetPull(db, repoID, 99); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestListPullsFilterByState(t *testing.T) {
	db, repoID := seedRepo(t)
	open := mustCreatePull(t, db, repoID, "main", "f1", "open one")
	closed := mustCreatePull(t, db, repoID, "main", "f2", "to close")
	merged := mustCreatePull(t, db, repoID, "main", "f3", "to merge")

	closedState := api.PRClosed
	if _, err := UpdatePull(db, repoID, closed.Number, nil, nil, &closedState); err != nil {
		t.Fatalf("close: %v", err)
	}
	mergedState := api.PRMerged
	if _, err := UpdatePull(db, repoID, merged.Number, nil, nil, &mergedState); err != nil {
		t.Fatalf("merge: %v", err)
	}

	all, err := ListPulls(db, repoID, nil, "")
	if err != nil {
		t.Fatalf("ListPulls all: %v", err)
	}
	// Newest number first.
	if len(all) != 3 || all[0].Number != merged.Number {
		t.Fatalf("all = %d rows, first #%d, want 3 newest-first", len(all), all[0].Number)
	}

	onlyOpen, err := ListPulls(db, repoID, []api.PRState{api.PROpen}, "")
	if err != nil {
		t.Fatalf("ListPulls open: %v", err)
	}
	if len(onlyOpen) != 1 || onlyOpen[0].Number != open.Number {
		t.Fatalf("open filter = %+v, want [#%d]", onlyOpen, open.Number)
	}

	openOrMerged, err := ListPulls(db, repoID, []api.PRState{api.PROpen, api.PRMerged}, "")
	if err != nil {
		t.Fatalf("ListPulls open|merged: %v", err)
	}
	if len(openOrMerged) != 2 {
		t.Fatalf("open|merged filter = %d rows, want 2", len(openOrMerged))
	}
}

// TestListPullsFilterByQuery covers the case-insensitive title/body keyword
// filter, including that it composes with the state filter and that LIKE
// wildcards in the term are matched literally.
func TestListPullsFilterByQuery(t *testing.T) {
	db, repoID := seedRepo(t)

	auth := mustCreatePull(t, db, repoID, "main", "f1", "Add auth middleware")
	mustCreatePull(t, db, repoID, "main", "f2", "Refactor parser")
	// Keyword lives only in the body, not the title.
	body, err := CreatePull(db, repoID, api.CreatePullRequest{
		Base: "main", Head: "f3", Title: "Tweak config", Body: "wires up the auth token", Author: "alice",
	})
	if err != nil {
		t.Fatalf("CreatePull with body: %v", err)
	}
	// Title containing a literal % — must not be treated as a wildcard.
	pct := mustCreatePull(t, db, repoID, "main", "f4", "Bump coverage to 80%")

	// Matches the title hit and the body-only hit, case-insensitively; excludes
	// the parser PR. Newest number first.
	got, err := ListPulls(db, repoID, nil, "AUTH")
	if err != nil {
		t.Fatalf("ListPulls query: %v", err)
	}
	if len(got) != 2 || got[0].Number != body.Number || got[1].Number != auth.Number {
		t.Fatalf("query 'AUTH' = %+v, want [#%d, #%d]", got, body.Number, auth.Number)
	}

	// Composes with the state filter: closing the body PR drops it from an
	// open-only query.
	closed := api.PRClosed
	if _, err := UpdatePull(db, repoID, body.Number, nil, nil, &closed); err != nil {
		t.Fatalf("close: %v", err)
	}
	openAuth, err := ListPulls(db, repoID, []api.PRState{api.PROpen}, "auth")
	if err != nil {
		t.Fatalf("ListPulls open+query: %v", err)
	}
	if len(openAuth) != 1 || openAuth[0].Number != auth.Number {
		t.Fatalf("open+query = %+v, want [#%d]", openAuth, auth.Number)
	}

	// A % in the query is a literal, not a wildcard: "80%" matches only the
	// coverage PR, not every title.
	pctHit, err := ListPulls(db, repoID, nil, "80%")
	if err != nil {
		t.Fatalf("ListPulls literal-pct: %v", err)
	}
	if len(pctHit) != 1 || pctHit[0].Number != pct.Number {
		t.Fatalf("query '80%%' = %+v, want [#%d]", pctHit, pct.Number)
	}
}

func TestUpdatePullPartialAndMergedAtInvariant(t *testing.T) {
	db, repoID := seedRepo(t)
	pr := mustCreatePull(t, db, repoID, "main", "feature", "title")

	// Title/body only — state and merged_at untouched.
	newTitle, newBody := "edited", "now with body"
	got, err := UpdatePull(db, repoID, pr.Number, &newTitle, &newBody, nil)
	if err != nil {
		t.Fatalf("update title/body: %v", err)
	}
	if got.Title != "edited" || got.Body != "now with body" {
		t.Errorf("title/body = %q/%q", got.Title, got.Body)
	}
	if got.State != api.PROpen || got.MergedAt != nil {
		t.Errorf("state/merged = %q/%v, want open/nil", got.State, got.MergedAt)
	}

	// -> merged stamps merged_at.
	merged := api.PRMerged
	got, err = UpdatePull(db, repoID, pr.Number, nil, nil, &merged)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if got.State != api.PRMerged || got.MergedAt == nil {
		t.Errorf("merged: state=%q merged_at=%v, want merged/non-nil", got.State, got.MergedAt)
	}

	// -> reopened clears merged_at (invariant: set iff merged).
	open := api.PROpen
	got, err = UpdatePull(db, repoID, pr.Number, nil, nil, &open)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got.State != api.PROpen || got.MergedAt != nil {
		t.Errorf("reopened: state=%q merged_at=%v, want open/nil", got.State, got.MergedAt)
	}
}

func TestUpdatePullNoFields(t *testing.T) {
	db, repoID := seedRepo(t)
	pr := mustCreatePull(t, db, repoID, "main", "feature", "title")
	if _, err := UpdatePull(db, repoID, pr.Number, nil, nil, nil); !errors.Is(err, ErrNoUpdateFields) {
		t.Errorf("err = %v, want ErrNoUpdateFields", err)
	}
}

func TestUpdatePullNotFound(t *testing.T) {
	db, repoID := seedRepo(t)
	title := "x"
	if _, err := UpdatePull(db, repoID, 99, &title, nil, nil); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
