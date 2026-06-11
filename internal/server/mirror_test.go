package server

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

// bareGit runs a git command in a bare repo dir, failing the test on error.
func bareGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v: %s", args, dir, err, out)
	}
}

func TestHasMirrorRemote(t *testing.T) {
	_, bare := newMergeTestServer(t)
	if hasMirrorRemote(bare) {
		t.Fatal("fresh bare repo reports a mirror remote; want none")
	}
	bareGit(t, bare, "remote", "add", "mirror", "https://example.invalid/x.git")
	if !hasMirrorRemote(bare) {
		t.Fatal("mirror remote added but hasMirrorRemote = false")
	}
}

func TestHasMirrorRemoteIgnoresOtherRemotes(t *testing.T) {
	_, bare := newMergeTestServer(t)
	// A remote whose name merely contains "mirror" must not count.
	bareGit(t, bare, "remote", "add", "mirrorx", "https://example.invalid/x.git")
	if hasMirrorRemote(bare) {
		t.Fatal("hasMirrorRemote matched a non-exact remote name")
	}
}

func TestPushMirrorSyncsBranch(t *testing.T) {
	_, bare := newMergeTestServer(t)
	// A second bare repo stands in for GitHub. Clone from the server bare so the
	// histories share a root (a real mirror would), then point the server bare's
	// "mirror" remote at it.
	mirror := filepath.Join(t.TempDir(), "mirror.git")
	bareGit(t, t.TempDir(), "clone", "-q", "--bare", bare, mirror)
	bareGit(t, bare, "remote", "add", "mirror", mirror)

	// Advance the server's main (fast-forward it to the `ahead` branch tip), the
	// same kind of ref move a server-side merge makes.
	aheadTip := bareRev(t, bare, "refs/heads/ahead")
	bareGit(t, bare, "update-ref", "refs/heads/main", aheadTip)
	if got := bareRev(t, mirror, "refs/heads/main"); got == aheadTip {
		t.Fatal("mirror already matched before pushMirror — test setup wrong")
	}

	if err := pushMirror(context.Background(), bare, "main"); err != nil {
		t.Fatalf("pushMirror: %v", err)
	}
	if got := bareRev(t, mirror, "refs/heads/main"); got != aheadTip {
		t.Fatalf("mirror main = %s after push, want %s", got, aheadTip)
	}
}

func TestPushMirrorRejectsDivergedMirror(t *testing.T) {
	_, bare := newMergeTestServer(t)
	mirror := filepath.Join(t.TempDir(), "mirror.git")
	bareGit(t, t.TempDir(), "clone", "-q", "--bare", bare, mirror)
	bareGit(t, bare, "remote", "add", "mirror", mirror)

	// Diverge the mirror: move its main to an unrelated commit (the conflict
	// branch is not an ancestor of the server's main), so a plain push is non-ff.
	conflictTip := bareRev(t, bare, "refs/heads/conflict")
	bareGit(t, mirror, "update-ref", "refs/heads/main", conflictTip)

	// Advance the server main so there's something to push.
	aheadTip := bareRev(t, bare, "refs/heads/ahead")
	bareGit(t, bare, "update-ref", "refs/heads/main", aheadTip)

	// Plain (non-force) push must be rejected rather than clobbering the mirror.
	if err := pushMirror(context.Background(), bare, "main"); err == nil {
		t.Fatal("pushMirror to a diverged mirror succeeded; want non-fast-forward rejection")
	}
}
