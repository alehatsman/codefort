package storage

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func newVerifyDB(t *testing.T) (*sql.DB, int64) {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "v.db"))
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

func TestVerificationsRecordLatestAndHistory(t *testing.T) {
	db, repoID := newVerifyDB(t)

	// No verification yet → ErrNotFound.
	if _, err := LatestVerification(db, repoID, "ssh-transport"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("LatestVerification on empty = %v, want ErrNotFound", err)
	}

	first, err := RecordVerification(db, SpecVerification{
		RepoID: repoID, SpecID: "ssh-transport", SpecPath: "specs/ssh-transport.md",
		CommitSHA: "aaaa", Alignment: 0.5, Result: `{"alignment":0.5}`, Verifier: "agent#1",
	})
	if err != nil {
		t.Fatalf("RecordVerification: %v", err)
	}
	if first.ID == 0 || first.CreatedAt == 0 {
		t.Errorf("stored row missing id/created_at: %+v", first)
	}

	second, err := RecordVerification(db, SpecVerification{
		RepoID: repoID, SpecID: "ssh-transport", SpecPath: "specs/ssh-transport.md",
		CommitSHA: "bbbb", Alignment: 0.91, // empty Result defaults to "{}"
	})
	if err != nil {
		t.Fatalf("RecordVerification 2: %v", err)
	}
	if second.Result != "{}" {
		t.Errorf("empty result = %q, want {}", second.Result)
	}

	// Latest is the most recent by id.
	latest, err := LatestVerification(db, repoID, "ssh-transport")
	if err != nil {
		t.Fatalf("LatestVerification: %v", err)
	}
	if latest.CommitSHA != "bbbb" || latest.Alignment != 0.91 {
		t.Errorf("latest = %+v, want commit bbbb / alignment 0.91", latest)
	}

	// History is newest-first and scoped to the spec id.
	hist, err := ListVerifications(db, repoID, "ssh-transport", 0)
	if err != nil {
		t.Fatalf("ListVerifications: %v", err)
	}
	if len(hist) != 2 || hist[0].CommitSHA != "bbbb" || hist[1].CommitSHA != "aaaa" {
		t.Errorf("history = %+v, want [bbbb, aaaa]", hist)
	}

	// A different spec id is isolated.
	if _, err := LatestVerification(db, repoID, "other"); !errors.Is(err, ErrNotFound) {
		t.Errorf("LatestVerification(other) = %v, want ErrNotFound", err)
	}
}
