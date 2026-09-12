package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// protectedRepo sets up a bare repo with the managed pre-receive hook and a
// clone of it carrying one commit on main, already pushed. It returns the bare
// repo path and the clone's working directory.
func protectedRepo(t *testing.T) (bare, work string) {
	t.Helper()
	requireBinaries(t, "git", "sh")

	root := t.TempDir()
	bare = filepath.Join(root, "proj.git")
	if out, err := exec.Command("git", "init", "-q", "--bare", "-b", "main", bare).CombinedOutput(); err != nil {
		t.Fatalf("git init bare: %v: %s", err, out)
	}
	if err := WriteManagedHooks(bare); err != nil {
		t.Fatalf("WriteManagedHooks: %v", err)
	}

	// git refuses to delete a bare repo's HEAD branch on its own. That rule
	// would mask this hook's refusal (and its absence), so switch it off and
	// let the assertions speak to the hook alone.
	mustGit(t, nil, bare, "config", "receive.denyDeleteCurrent", "ignore")

	work = filepath.Join(root, "work")
	if out, err := exec.Command("git", "clone", "-q", bare, work).CombinedOutput(); err != nil {
		t.Fatalf("git clone: %v: %s", err, out)
	}
	mustGit(t, nil, work, "config", "user.email", "alice@example.com")
	mustGit(t, nil, work, "config", "user.name", "alice")
	commit(t, work, "one")
	mustGit(t, nil, work, "push", "-q", "origin", "main")
	return bare, work
}

// commit writes a file named for msg and commits it, so each call produces a
// distinct tree.
func commit(t *testing.T, work, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(work, msg+".txt"), []byte(msg+"\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", msg, err)
	}
	mustGit(t, nil, work, "add", ".")
	mustGit(t, nil, work, "commit", "-q", "-m", msg)
}

// push runs `git push` with the protection patterns in the environment, the
// way moongitd injects them into receive-pack. A local-path push runs
// receive-pack as a child of push, so it inherits this environment exactly as
// the hook does on the server.
func push(t *testing.T, work, patterns string, args ...string) (string, error) {
	t.Helper()
	env := append(os.Environ(), "CODEFORT_PROTECTED_REFS="+patterns)
	return runEnv(env, work, "git", append([]string{"push"}, args...)...)
}

// tip returns a ref's SHA in the bare repo, or "" when the ref is absent.
func tip(t *testing.T, bare, ref string) string {
	t.Helper()
	out, err := runEnv(nil, bare, "git", "rev-parse", "--verify", "-q", ref)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// With no patterns configured the hook is inert: the two destructive pushes it
// would otherwise refuse both succeed, which is the "an existing deployment is
// unchanged" clause.
func TestProtectionOffAllowsForceAndDelete(t *testing.T) {
	bare, work := protectedRepo(t)
	first := tip(t, bare, "refs/heads/main")

	mustGit(t, nil, work, "reset", "-q", "--hard", "HEAD~0")
	commit(t, work, "two")
	mustGit(t, nil, work, "reset", "-q", "--hard", "HEAD~1")
	commit(t, work, "three")
	if out, err := push(t, work, "", "-q", "--force", "origin", "main"); err != nil {
		t.Fatalf("force push refused with no patterns: %v\n%s", err, out)
	}
	if got := tip(t, bare, "refs/heads/main"); got == first {
		t.Error("force push left the tip unchanged")
	}

	if out, err := push(t, work, "", "-q", "origin", ":main"); err != nil {
		t.Fatalf("delete refused with no patterns: %v\n%s", err, out)
	}
	if got := tip(t, bare, "refs/heads/main"); got != "" {
		t.Errorf("main survived the delete: %s", got)
	}
}

func TestProtectionRefusesDelete(t *testing.T) {
	bare, work := protectedRepo(t)
	before := tip(t, bare, "refs/heads/main")

	out, err := push(t, work, "main", "-q", "origin", ":main")
	if err == nil {
		t.Fatalf("delete of a protected branch succeeded:\n%s", out)
	}
	if !strings.Contains(out, "refusing to delete") {
		t.Errorf("refusal did not explain itself:\n%s", out)
	}
	if got := tip(t, bare, "refs/heads/main"); got != before {
		t.Errorf("main moved during a refused delete: %s -> %s", before, got)
	}
}

func TestProtectionRefusesNonFastForward(t *testing.T) {
	bare, work := protectedRepo(t)
	commit(t, work, "two")
	mustGit(t, nil, work, "push", "-q", "origin", "main")
	before := tip(t, bare, "refs/heads/main")

	// Rewind and build a divergent commit — pushing it would drop "two".
	mustGit(t, nil, work, "reset", "-q", "--hard", "HEAD~1")
	commit(t, work, "other")

	out, err := push(t, work, "main", "-q", "--force", "origin", "main")
	if err == nil {
		t.Fatalf("force push to a protected branch succeeded:\n%s", out)
	}
	if !strings.Contains(out, "non-fast-forward") {
		t.Errorf("refusal did not explain itself:\n%s", out)
	}
	if got := tip(t, bare, "refs/heads/main"); got != before {
		t.Errorf("main moved during a refused force push: %s -> %s", before, got)
	}
}

// Protection guards history, not commits: advancing a protected branch and
// creating a new branch that matches the pattern both stay allowed.
func TestProtectionAllowsFastForwardAndCreate(t *testing.T) {
	bare, work := protectedRepo(t)
	before := tip(t, bare, "refs/heads/main")

	commit(t, work, "two")
	if out, err := push(t, work, "main\nrelease/*", "-q", "origin", "main"); err != nil {
		t.Fatalf("fast-forward refused on a protected branch: %v\n%s", err, out)
	}
	if got := tip(t, bare, "refs/heads/main"); got == before {
		t.Error("fast-forward did not move main")
	}

	mustGit(t, nil, work, "checkout", "-q", "-b", "release/1.0")
	commit(t, work, "rel")
	if out, err := push(t, work, "main\nrelease/*", "-q", "origin", "release/1.0"); err != nil {
		t.Fatalf("creating a matching branch refused: %v\n%s", err, out)
	}
	if tip(t, bare, "refs/heads/release/1.0") == "" {
		t.Error("release/1.0 was not created")
	}
}

// Patterns are globs over the branch name, so release/* covers release/1.0 but
// leaves an unrelated branch alone.
func TestProtectionPatternIsAGlobOverBranchNames(t *testing.T) {
	bare, work := protectedRepo(t)

	mustGit(t, nil, work, "checkout", "-q", "-b", "release/1.0")
	commit(t, work, "rel")
	mustGit(t, nil, work, "push", "-q", "origin", "release/1.0")
	protected := tip(t, bare, "refs/heads/release/1.0")

	mustGit(t, nil, work, "checkout", "-q", "-b", "scratch")
	commit(t, work, "scratch")
	mustGit(t, nil, work, "push", "-q", "origin", "scratch")

	if out, err := push(t, work, "release/*", "-q", "origin", ":release/1.0"); err == nil {
		t.Fatalf("release/1.0 was deletable under release/*:\n%s", out)
	}
	if got := tip(t, bare, "refs/heads/release/1.0"); got != protected {
		t.Errorf("release/1.0 moved: %s -> %s", protected, got)
	}
	if out, err := push(t, work, "release/*", "-q", "origin", ":scratch"); err != nil {
		t.Fatalf("unmatched branch was protected anyway: %v\n%s", err, out)
	}
	if got := tip(t, bare, "refs/heads/scratch"); got != "" {
		t.Errorf("scratch survived its delete: %s", got)
	}
}

// Only refs/heads/* is in scope — a tag named like a protected branch is not
// protected.
func TestProtectionIgnoresTags(t *testing.T) {
	bare, work := protectedRepo(t)
	mustGit(t, nil, work, "tag", "main")
	mustGit(t, nil, work, "push", "-q", "origin", "refs/tags/main")
	if tip(t, bare, "refs/tags/main") == "" {
		t.Fatal("tag was not pushed")
	}
	if out, err := push(t, work, "main", "-q", "origin", ":refs/tags/main"); err != nil {
		t.Fatalf("tag delete refused under a branch pattern: %v\n%s", err, out)
	}
	if got := tip(t, bare, "refs/tags/main"); got != "" {
		t.Errorf("tag survived its delete: %s", got)
	}
}

// A pre-receive refusal rejects the whole push, so an allowed ref riding along
// with a refused one does not land either.
func TestProtectionRejectsTheWholePush(t *testing.T) {
	bare, work := protectedRepo(t)
	mustGit(t, nil, work, "checkout", "-q", "-b", "feature")
	commit(t, work, "feat")

	out, err := push(t, work, "main", "-q", "origin", "feature", ":main")
	if err == nil {
		t.Fatalf("mixed push with a protected delete succeeded:\n%s", out)
	}
	if got := tip(t, bare, "refs/heads/feature"); got != "" {
		t.Errorf("feature landed despite the rejected push: %s", got)
	}
	if tip(t, bare, "refs/heads/main") == "" {
		t.Error("main was deleted despite the rejected push")
	}
}

func TestJoinPatterns(t *testing.T) {
	tests := []struct {
		name  string
		in    []string
		want  string
		isErr bool
	}{
		{name: "empty clears", in: []string{}, want: ""},
		{name: "trims and drops blanks", in: []string{" main ", "", "  ", "release/*"}, want: "main\nrelease/*"},
		{name: "embedded newline rejected", in: []string{"main\nrelease/*"}, isErr: true},
		{name: "NUL rejected", in: []string{"main\x00evil"}, isErr: true},
		{name: "over-long rejected", in: []string{strings.Repeat("x", maxProtectedRefLen+1)}, isErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := joinPatterns(tc.in)
			if tc.isErr {
				if err == nil {
					t.Fatalf("joinPatterns(%q) = %q, want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("joinPatterns(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("joinPatterns(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}

	many := make([]string, maxProtectedRefs+1)
	for i := range many {
		many[i] = "b" + string(rune('a'+i%26))
	}
	if _, err := joinPatterns(many); err == nil {
		t.Errorf("joinPatterns accepted %d patterns, want error over %d", len(many), maxProtectedRefs)
	}
}

func TestSplitPatternsIsNeverNil(t *testing.T) {
	got := splitPatterns("")
	if got == nil {
		t.Fatal("splitPatterns(\"\") = nil, want an empty slice so the JSON is []")
	}
	if len(got) != 0 {
		t.Errorf("splitPatterns(\"\") = %q, want empty", got)
	}
	if got := splitPatterns("main\n\nrelease/*\n"); len(got) != 2 || got[0] != "main" || got[1] != "release/*" {
		t.Errorf("splitPatterns dropped or kept the wrong lines: %q", got)
	}
}
