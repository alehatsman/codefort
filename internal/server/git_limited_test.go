package server

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// gitOutputLimited must cap the bytes it buffers (the OOM guard for #196) yet
// return the full output when it fits under the budget.
func TestGitOutputLimited(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(cmd.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v (%s)", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q")
	// A blob comfortably larger than the small budget below, with newlines so the
	// partial-line trim has something to cut.
	blob := strings.Repeat("the quick brown fox\n", 1000) // 20 KB
	if err := os.WriteFile(dir+"/big.txt", []byte(blob), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "big.txt")
	run("commit", "-q", "-m", "add big")

	ctx := context.Background()

	t.Run("under budget returns full output untruncated", func(t *testing.T) {
		out, truncated, err := gitOutputLimited(ctx, dir, 1<<20, "show", "HEAD:big.txt")
		if err != nil {
			t.Fatalf("gitOutputLimited: %v", err)
		}
		if truncated {
			t.Errorf("output of %d bytes flagged truncated under a 1 MiB budget", len(out))
		}
		if string(out) != blob {
			t.Errorf("output not returned whole: got %d bytes, want %d", len(out), len(blob))
		}
	})

	t.Run("over budget truncates to a whole-line boundary", func(t *testing.T) {
		const budget = 256
		out, truncated, err := gitOutputLimited(ctx, dir, budget, "show", "HEAD:big.txt")
		if err != nil {
			t.Fatalf("gitOutputLimited: %v", err)
		}
		if !truncated {
			t.Fatal("a 20 KB blob under a 256-byte budget should be truncated")
		}
		if len(out) > budget {
			t.Errorf("kept %d bytes, want <= %d", len(out), budget)
		}
		// Partial trailing line dropped: the kept bytes end on a newline.
		if len(out) > 0 && out[len(out)-1] != '\n' {
			t.Errorf("truncated output should end on a line boundary, got %q tail", out[len(out)-1:])
		}
		if !bytes.HasPrefix([]byte(blob), out) {
			t.Error("truncated output should be a prefix of the full blob")
		}
	})

	t.Run("real git error surfaces", func(t *testing.T) {
		if _, _, err := gitOutputLimited(ctx, dir, 1<<20, "show", "HEAD:nope.txt"); err == nil {
			t.Error("expected an error for a missing path")
		}
	})
}
