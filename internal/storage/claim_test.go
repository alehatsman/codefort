package storage

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/alehatsman/moongit/internal/api"
)

// seedIssue spins up a migrated temp DB with one repo and one issue, returning
// the writer handle and the issue's repo id + number.
func seedIssue(t *testing.T) (db *sql.DB, repoID int64, number int) {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "claim.db"))
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
	iss, err := CreateIssue(d, id, api.CreateIssueRequest{Title: "t", Author: "alice"})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	return d, id, iss.Number
}

const testLease = time.Hour

func TestClaimUnassignedSucceeds(t *testing.T) {
	db, repoID, num := seedIssue(t)
	iss, err := Claim(db, repoID, num, "agent-a", "", testLease)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if iss.Assignee == nil || *iss.Assignee != "agent-a" {
		t.Fatalf("assignee = %v, want agent-a", iss.Assignee)
	}
	if iss.ClaimedAt == nil {
		t.Fatal("claimed_at not set on successful claim")
	}
}

func TestClaimLiveClaimByOtherConflicts(t *testing.T) {
	db, repoID, num := seedIssue(t)
	if _, err := Claim(db, repoID, num, "agent-a", "", testLease); err != nil {
		t.Fatalf("first Claim: %v", err)
	}
	_, err := Claim(db, repoID, num, "agent-b", "", testLease)
	if !errors.Is(err, ErrAlreadyClaimed) {
		t.Fatalf("second Claim err = %v, want ErrAlreadyClaimed", err)
	}
}

func TestClaimHeartbeatRefreshesLease(t *testing.T) {
	db, repoID, num := seedIssue(t)
	if _, err := Claim(db, repoID, num, "agent-a", "", testLease); err != nil {
		t.Fatalf("first Claim: %v", err)
	}
	// Age the claim into the past, then have the same owner re-claim.
	if _, err := db.Exec(`UPDATE issues SET claimed_at = claimed_at - 999999 WHERE repo_id = ? AND number = ?`, repoID, num); err != nil {
		t.Fatalf("age claim: %v", err)
	}
	iss, err := Claim(db, repoID, num, "agent-a", "", testLease)
	if err != nil {
		t.Fatalf("heartbeat Claim: %v", err)
	}
	// Lease must be refreshed to ~now (not the aged value).
	if iss.ClaimedAt == nil || time.Since(*iss.ClaimedAt) > time.Minute {
		t.Fatalf("heartbeat did not refresh claimed_at: %v", iss.ClaimedAt)
	}
}

func TestClaimExpiredLeaseCanBeStolen(t *testing.T) {
	db, repoID, num := seedIssue(t)
	if _, err := Claim(db, repoID, num, "agent-a", "", testLease); err != nil {
		t.Fatalf("first Claim: %v", err)
	}
	// Push the claim past its lease, then a different agent steals it.
	if _, err := db.Exec(`UPDATE issues SET claimed_at = claimed_at - 999999 WHERE repo_id = ? AND number = ?`, repoID, num); err != nil {
		t.Fatalf("age claim: %v", err)
	}
	iss, err := Claim(db, repoID, num, "agent-b", "", testLease)
	if err != nil {
		t.Fatalf("steal Claim: %v", err)
	}
	if iss.Assignee == nil || *iss.Assignee != "agent-b" {
		t.Fatalf("assignee = %v, want agent-b after steal", iss.Assignee)
	}
}

func TestClaimZeroLeaseNeverExpires(t *testing.T) {
	db, repoID, num := seedIssue(t)
	if _, err := Claim(db, repoID, num, "agent-a", "", 0); err != nil {
		t.Fatalf("first Claim: %v", err)
	}
	if _, err := db.Exec(`UPDATE issues SET claimed_at = claimed_at - 999999 WHERE repo_id = ? AND number = ?`, repoID, num); err != nil {
		t.Fatalf("age claim: %v", err)
	}
	// Even an ancient claim holds when lease is disabled.
	_, err := Claim(db, repoID, num, "agent-b", "", 0)
	if !errors.Is(err, ErrAlreadyClaimed) {
		t.Fatalf("steal with zero lease err = %v, want ErrAlreadyClaimed", err)
	}
}

func TestClaimNotFound(t *testing.T) {
	db, repoID, _ := seedIssue(t)
	_, err := Claim(db, repoID, 999, "agent-a", "", testLease)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Claim missing issue err = %v, want ErrNotFound", err)
	}
}

func TestUnclaimByOwnerClears(t *testing.T) {
	db, repoID, num := seedIssue(t)
	if _, err := Claim(db, repoID, num, "agent-a", "", testLease); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	iss, err := Unclaim(db, repoID, num, "agent-a")
	if err != nil {
		t.Fatalf("Unclaim: %v", err)
	}
	if iss.Assignee != nil {
		t.Fatalf("assignee = %v, want nil after unclaim", iss.Assignee)
	}
	if iss.ClaimedAt != nil {
		t.Fatalf("claimed_at = %v, want nil after unclaim", iss.ClaimedAt)
	}
}

func TestUnclaimByNonOwnerRejected(t *testing.T) {
	db, repoID, num := seedIssue(t)
	if _, err := Claim(db, repoID, num, "agent-a", "", testLease); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	_, err := Unclaim(db, repoID, num, "agent-b")
	if !errors.Is(err, ErrNotOwner) {
		t.Fatalf("Unclaim by other err = %v, want ErrNotOwner", err)
	}
	// The claim must still belong to agent-a.
	iss, err := GetIssue(db, repoID, num)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if iss.Assignee == nil || *iss.Assignee != "agent-a" {
		t.Fatalf("assignee = %v, want agent-a (claim must survive)", iss.Assignee)
	}
}

func TestUnclaimAlreadyUnclaimedIsIdempotent(t *testing.T) {
	db, repoID, num := seedIssue(t)
	iss, err := Unclaim(db, repoID, num, "agent-a")
	if err != nil {
		t.Fatalf("Unclaim of unassigned err = %v, want nil", err)
	}
	if iss.Assignee != nil {
		t.Fatalf("assignee = %v, want nil", iss.Assignee)
	}
}

func TestUnclaimNotFound(t *testing.T) {
	db, repoID, _ := seedIssue(t)
	_, err := Unclaim(db, repoID, 999, "agent-a")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Unclaim missing issue err = %v, want ErrNotFound", err)
	}
}

func TestExpireClaimsReleasesOnlyExpired(t *testing.T) {
	db, repoID, num := seedIssue(t)
	// A second, freshly-claimed issue that must survive the sweep.
	fresh, err := CreateIssue(db, repoID, api.CreateIssueRequest{Title: "fresh", Author: "alice"})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	if _, err := Claim(db, repoID, num, "agent-a", "", testLease); err != nil {
		t.Fatalf("Claim aged: %v", err)
	}
	if _, err := Claim(db, repoID, fresh.Number, "agent-b", "", testLease); err != nil {
		t.Fatalf("Claim fresh: %v", err)
	}
	// Age only the first claim past the lease.
	if _, err := db.Exec(`UPDATE issues SET claimed_at = claimed_at - 999999 WHERE repo_id = ? AND number = ?`, repoID, num); err != nil {
		t.Fatalf("age claim: %v", err)
	}

	n, err := ExpireClaims(db, testLease)
	if err != nil {
		t.Fatalf("ExpireClaims: %v", err)
	}
	if n != 1 {
		t.Fatalf("released %d claims, want 1", n)
	}

	aged, _ := GetIssue(db, repoID, num)
	if aged.Assignee != nil || aged.ClaimedAt != nil {
		t.Fatalf("expired claim not released: assignee=%v claimed_at=%v", aged.Assignee, aged.ClaimedAt)
	}
	live, _ := GetIssue(db, repoID, fresh.Number)
	if live.Assignee == nil || *live.Assignee != "agent-b" {
		t.Fatalf("fresh claim wrongly released: assignee=%v", live.Assignee)
	}
}

func TestExpireClaimsZeroLeaseIsNoop(t *testing.T) {
	db, repoID, num := seedIssue(t)
	if _, err := Claim(db, repoID, num, "agent-a", "", testLease); err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if _, err := db.Exec(`UPDATE issues SET claimed_at = claimed_at - 999999 WHERE repo_id = ? AND number = ?`, repoID, num); err != nil {
		t.Fatalf("age claim: %v", err)
	}
	n, err := ExpireClaims(db, 0)
	if err != nil {
		t.Fatalf("ExpireClaims: %v", err)
	}
	if n != 0 {
		t.Fatalf("released %d with zero lease, want 0 (disabled)", n)
	}
}
