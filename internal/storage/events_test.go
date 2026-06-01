package storage

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// seedEventsRepo spins up a migrated temp DB with one repo, returning the
// writer handle and the repo id.
func seedEventsRepo(t *testing.T) (db *sql.DB, repoID int64) {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "events.db"))
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

func TestAppendAndListEvents(t *testing.T) {
	db, repoID := seedEventsRepo(t)

	s1, err := AppendEvent(db, "issue.created", repoID, "alice", `{"number":1}`)
	if err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	s2, err := AppendEvent(db, "push", repoID, "bob", `{"ref":"main"}`)
	if err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if s2 <= s1 {
		t.Fatalf("seq not monotonic: s1=%d s2=%d", s1, s2)
	}

	// Replay from the start resolves the repo slug and preserves order.
	all, err := ListEventsSince(db, 0, 0, 0)
	if err != nil {
		t.Fatalf("ListEventsSince: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("want 2 events, got %d", len(all))
	}
	if all[0].Type != "issue.created" || all[1].Type != "push" {
		t.Fatalf("unexpected order/types: %+v", all)
	}
	if all[0].Owner != "alice" || all[0].Repo != "repo" {
		t.Fatalf("repo slug not resolved: owner=%q repo=%q", all[0].Owner, all[0].Repo)
	}
	if all[0].Actor != "alice" || all[0].Payload != `{"number":1}` {
		t.Fatalf("actor/payload mismatch: %+v", all[0])
	}

	// Resume past the first event yields only the second.
	rest, err := ListEventsSince(db, s1, 0, 0)
	if err != nil {
		t.Fatalf("ListEventsSince resume: %v", err)
	}
	if len(rest) != 1 || rest[0].Seq != s2 {
		t.Fatalf("resume want [%d], got %+v", s2, rest)
	}
}

func TestAppendEventEmptyPayloadNormalized(t *testing.T) {
	db, repoID := seedEventsRepo(t)
	if _, err := AppendEvent(db, "issue.claimed", repoID, "alice", ""); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	got, err := ListEventsSince(db, 0, 0, 0)
	if err != nil {
		t.Fatalf("ListEventsSince: %v", err)
	}
	if len(got) != 1 || got[0].Payload != "{}" {
		t.Fatalf("empty payload not normalized: %+v", got)
	}
}

func TestListEventsRepoFilter(t *testing.T) {
	db, repoA := seedEventsRepo(t)
	repoB, err := EnsureRepo(db, "alice", "other")
	if err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}
	if _, err := AppendEvent(db, "push", repoA, "alice", `{}`); err != nil {
		t.Fatalf("AppendEvent A: %v", err)
	}
	if _, err := AppendEvent(db, "push", repoB, "alice", `{}`); err != nil {
		t.Fatalf("AppendEvent B: %v", err)
	}

	only, err := ListEventsSince(db, 0, repoB, 0)
	if err != nil {
		t.Fatalf("ListEventsSince filter: %v", err)
	}
	if len(only) != 1 || only[0].Repo != "other" {
		t.Fatalf("repo filter leaked: %+v", only)
	}
}

func TestPruneEventsKeepsNewest(t *testing.T) {
	db, repoID := seedEventsRepo(t)
	for range 10 {
		if _, err := AppendEvent(db, "push", repoID, "alice", `{}`); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
	}

	deleted, err := PruneEvents(db, 3)
	if err != nil {
		t.Fatalf("PruneEvents: %v", err)
	}
	if deleted != 7 {
		t.Fatalf("want 7 pruned, got %d", deleted)
	}
	remaining, err := ListEventsSince(db, 0, 0, 0)
	if err != nil {
		t.Fatalf("ListEventsSince: %v", err)
	}
	if len(remaining) != 3 {
		t.Fatalf("want 3 remaining, got %d", len(remaining))
	}
	// The survivors are the newest three (highest seqs), still ascending.
	if remaining[0].Seq != 8 || remaining[2].Seq != 10 {
		t.Fatalf("pruned the wrong rows: %+v", remaining)
	}

	// keep <= 0 disables pruning.
	if n, err := PruneEvents(db, 0); err != nil || n != 0 {
		t.Fatalf("PruneEvents(0) = %d, %v; want 0, nil", n, err)
	}
}
