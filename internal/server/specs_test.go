package server

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/alehatsman/moongit/internal/api"
	"github.com/alehatsman/moongit/internal/config"
	"github.com/alehatsman/moongit/internal/storage"
)

// newSpecsTestServer builds a server backed by a bare repo whose working tree
// is seeded by write(). It returns the server and the seeding/committing
// helpers so each test can shape its own specs/ layout, then bare-clone.
func newSpecsTestServer(t *testing.T) (s *Server, write func(rel, content string), commitAndClone func()) {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := storage.EnsureRepo(db, cOwner, cRepo); err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}

	reposDir := t.TempDir()
	work := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = work
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Alice", "GIT_AUTHOR_EMAIL=a@b.c",
			"GIT_COMMITTER_NAME=Alice", "GIT_COMMITTER_EMAIL=a@b.c",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")

	write = func(rel, content string) {
		t.Helper()
		p := filepath.Join(work, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	commitAndClone = func() {
		t.Helper()
		git("add", ".")
		git("commit", "-q", "-m", "seed")
		bare := filepath.Join(reposDir, cOwner, cRepo+".git")
		if err := os.MkdirAll(filepath.Dir(bare), 0o755); err != nil {
			t.Fatal(err)
		}
		git("clone", "-q", "--bare", work, bare)
	}

	s = &Server{
		cfg:    &config.Config{ReposDir: reposDir},
		db:     db,
		rdb:    db,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return s, write, commitAndClone
}

func TestHandleListSpecs(t *testing.T) {
	s, write, done := newSpecsTestServer(t)
	write("specs/ssh-transport.md", `---
id: ssh-transport
status: living
owners: [aleh]
covers: ["internal/ssh/**"]
last_verified: 2026-06-02
alignment: 0.91
---
# SSH Transport
## Intent
Git over SSH.
`)
	// A foldered spec — must be found recursively.
	write("specs/ci/pipeline.md", "# Pipeline\n## Intent\nCI DAG.\n")
	// A non-frontmatter spec — title from its H1, id from the path.
	write("specs/plain.md", "# Plain Spec\nbody\n")
	// A non-markdown file under specs/ — must be ignored.
	write("specs/diagram.png", "not markdown")
	// A markdown file outside specs/ — must not appear.
	write("README.md", "# Readme\n")
	done()

	out, code := getJSON[api.SpecList](t, s, s.handleListSpecs, "/api/repos/alice/proj/specs")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if out.Ref != "main" {
		t.Errorf("ref = %q, want main", out.Ref)
	}
	if len(out.Specs) != 3 {
		t.Fatalf("got %d specs, want 3: %+v", len(out.Specs), out.Specs)
	}

	// Sorted by path: specs/ci/pipeline.md, specs/plain.md, specs/ssh-transport.md.
	if out.Specs[0].Path != "specs/ci/pipeline.md" || out.Specs[0].ID != "pipeline" {
		t.Errorf("spec[0] = %+v", out.Specs[0])
	}
	if out.Specs[1].Path != "specs/plain.md" || out.Specs[1].Title != "Plain Spec" || out.Specs[1].ID != "plain" {
		t.Errorf("spec[1] = %+v", out.Specs[1])
	}

	ssh := out.Specs[2]
	if ssh.Path != "specs/ssh-transport.md" || ssh.ID != "ssh-transport" {
		t.Fatalf("spec[2] = %+v", ssh)
	}
	if ssh.Title != "SSH Transport" || ssh.Status != "living" {
		t.Errorf("ssh title/status = %q/%q", ssh.Title, ssh.Status)
	}
	if len(ssh.Owners) != 1 || ssh.Owners[0] != "aleh" {
		t.Errorf("ssh owners = %v", ssh.Owners)
	}
	if len(ssh.Covers) != 1 || ssh.Covers[0] != "internal/ssh/**" {
		t.Errorf("ssh covers = %v", ssh.Covers)
	}
	if ssh.LastVerified != "2026-06-02" {
		t.Errorf("ssh last_verified = %q", ssh.LastVerified)
	}
	if ssh.Alignment == nil || *ssh.Alignment != 0.91 {
		t.Errorf("ssh alignment = %v", ssh.Alignment)
	}
}

func TestHandleListSpecsNoSpecsDir(t *testing.T) {
	// A repo with commits but no specs/ dir: empty list, 200, non-null Specs.
	s, write, done := newSpecsTestServer(t)
	write("README.md", "# Readme\n")
	done()

	out, code := getJSON[api.SpecList](t, s, s.handleListSpecs, "/api/repos/alice/proj/specs")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if out.Specs == nil {
		t.Fatal("Specs is nil; want empty non-null slice")
	}
	if len(out.Specs) != 0 {
		t.Errorf("got %d specs, want 0", len(out.Specs))
	}
}

func TestHandleListSpecsMalformedFrontmatter(t *testing.T) {
	// A spec with broken frontmatter YAML is still listed (path-derived title),
	// not dropped.
	s, write, done := newSpecsTestServer(t)
	write("specs/broken.md", "---\nid: [unterminated\n---\n# Broken\n")
	done()

	out, code := getJSON[api.SpecList](t, s, s.handleListSpecs, "/api/repos/alice/proj/specs")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if len(out.Specs) != 1 {
		t.Fatalf("got %d specs, want 1", len(out.Specs))
	}
	if out.Specs[0].Path != "specs/broken.md" || out.Specs[0].Title != "broken" {
		t.Errorf("broken spec = %+v", out.Specs[0])
	}
}
