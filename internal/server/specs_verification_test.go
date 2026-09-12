package server

import (
	"net/http"
	"testing"

	"github.com/alehatsman/codefort/internal/storage"
)

func TestGetSpecIncludesLatestVerification(t *testing.T) {
	s, write, done := newSpecsTestServer(t)
	write("specs/ssh-transport.md", "---\nid: ssh-transport\ncovers: [\"internal/ssh/**\"]\n---\n# SSH\n## Behavior\nWHEN x THEN y.\n")
	done()

	repoID, err := storage.EnsureRepo(s.db, cOwner, cRepo)
	if err != nil {
		t.Fatal(err)
	}
	// Record a verification at a bogus (now-absent) commit so the spec reads as
	// stale (the diff against a gone commit errs toward stale).
	if _, err := storage.RecordVerification(s.db, storage.SpecVerification{
		RepoID: repoID, SpecID: "ssh-transport", SpecPath: "specs/ssh-transport.md",
		CommitSHA: "deadbeef", Alignment: 0.75,
		Result:   `{"alignment":0.75,"markers":[{"line":6,"text":"WHEN x THEN y.","marker":"drifted","note":"server.go changed"}],"conflicts":["behavior drifted"]}`,
		Verifier: "codefort-agent",
	}); err != nil {
		t.Fatalf("RecordVerification: %v", err)
	}

	out, code := getSpec(t, s, "ssh-transport.md", "")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if out.Verification == nil {
		t.Fatal("Verification missing from spec content")
	}
	v := out.Verification
	if v.Alignment != 0.75 {
		t.Errorf("alignment = %v", v.Alignment)
	}
	if len(v.Markers) != 1 || v.Markers[0].Marker != "drifted" || v.Markers[0].Line != 6 {
		t.Errorf("markers = %+v", v.Markers)
	}
	if len(v.Conflicts) != 1 {
		t.Errorf("conflicts = %v", v.Conflicts)
	}
	if v.Commit != "deadbeef" || v.VerifiedAt == "" {
		t.Errorf("commit/verified_at = %q / %q", v.Commit, v.VerifiedAt)
	}
	if !v.Stale {
		t.Error("expected stale (verified against an absent commit)")
	}
}

func TestGetSpecNoVerification(t *testing.T) {
	s, write, done := newSpecsTestServer(t)
	write("specs/fresh.md", "# Fresh\n")
	done()

	out, code := getSpec(t, s, "fresh.md", "")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if out.Verification != nil {
		t.Errorf("unverified spec should have nil verification, got %+v", out.Verification)
	}
}
