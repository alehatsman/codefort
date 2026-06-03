package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alehatsman/moongit/internal/api"
	"github.com/alehatsman/moongit/internal/config"
	"github.com/alehatsman/moongit/internal/storage"
)

// newDriftServer builds a server over a bare repo with two commits: a baseline
// (C1) carrying the specs + governed files, then a change to internal/ssh only
// (C2 = HEAD). It records verifications at C1 for the specs that should have a
// baseline, so the pre-pass can classify fresh/stale/unverified/uncovered.
func newDriftServer(t *testing.T) *Server {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "d.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	repoID, err := storage.EnsureRepo(db, cOwner, cRepo)
	if err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}

	reposDir := t.TempDir()
	work := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = work
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Alice", "GIT_AUTHOR_EMAIL=a@b.c",
			"GIT_COMMITTER_NAME=Alice", "GIT_COMMITTER_EMAIL=a@b.c",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(work, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q", "-b", "main")
	write("specs/ssh.md", "---\nid: ssh\ncovers: [\"internal/ssh/**\"]\n---\n# SSH\n")
	write("specs/docs-spec.md", "---\nid: docs-spec\ncovers: [\"docs/**\"]\n---\n# Docs\n")
	write("specs/unv.md", "---\nid: unv\ncovers: [\"cmd/**\"]\n---\n# Unverified\n")
	write("specs/nocov.md", "---\nid: nocov\n---\n# No coverage\n")
	write("internal/ssh/server.go", "package ssh\n")
	write("docs/readme.md", "# docs\n")
	git("add", ".")
	git("commit", "-q", "-m", "C1")
	c1 := git("rev-parse", "HEAD")

	// Verify ssh + docs-spec at C1 (unv/nocov get no record).
	for _, id := range []string{"ssh", "docs-spec"} {
		if _, err := storage.RecordVerification(db, storage.SpecVerification{
			RepoID: repoID, SpecID: id, SpecPath: "specs/" + id + ".md", CommitSHA: c1, Alignment: 1,
		}); err != nil {
			t.Fatalf("RecordVerification %s: %v", id, err)
		}
	}

	// C2: change only governed-by-ssh code. docs/ untouched.
	write("internal/ssh/server.go", "package ssh\n\nfunc New() {}\n")
	git("add", ".")
	git("commit", "-q", "-m", "C2")

	bare := filepath.Join(reposDir, cOwner, cRepo+".git")
	if err := os.MkdirAll(filepath.Dir(bare), 0o755); err != nil {
		t.Fatal(err)
	}
	git("clone", "-q", "--bare", work, bare)

	return &Server{
		cfg:    &config.Config{ReposDir: reposDir},
		db:     db,
		rdb:    db,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestHandleSpecsDrift(t *testing.T) {
	s := newDriftServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/repos/alice/proj/specs/drift", nil)
	req.SetPathValue("owner", cOwner)
	req.SetPathValue("repo", cRepo)
	rr := httptest.NewRecorder()
	s.handleSpecsDrift(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s)", rr.Code, rr.Body.String())
	}
	var out api.SpecDriftReport
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}

	status := map[string]api.SpecDriftItem{}
	for _, it := range out.Specs {
		status[it.ID] = it
	}
	if got := status["ssh"].Status; got != "stale" {
		t.Errorf("ssh status = %q, want stale", got)
	}
	if chg := status["ssh"].Changed; len(chg) != 1 || chg[0] != "internal/ssh/server.go" {
		t.Errorf("ssh changed = %v, want [internal/ssh/server.go]", chg)
	}
	if got := status["docs-spec"].Status; got != "fresh" {
		t.Errorf("docs-spec status = %q, want fresh (docs/ unchanged since baseline)", got)
	}
	if got := status["unv"].Status; got != "unverified" {
		t.Errorf("unv status = %q, want unverified (covers but no record)", got)
	}
	if got := status["nocov"].Status; got != "uncovered" {
		t.Errorf("nocov status = %q, want uncovered (no covers)", got)
	}
}

func TestGitDiffGlobs(t *testing.T) {
	// Direct check of the glob pathspec matching against a small history.
	s := newDriftServer(t)
	bare := filepath.Join(s.cfg.ReposDir, cOwner, cRepo+".git")
	base, err := gitOutput(context.Background(), bare, "rev-parse", "HEAD~1")
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	baseSHA := strings.TrimSpace(string(base))

	changed, err := gitDiffGlobs(context.Background(), bare, baseSHA, "HEAD", []string{"internal/ssh/**"})
	if err != nil {
		t.Fatalf("gitDiffGlobs: %v", err)
	}
	if len(changed) != 1 || changed[0] != "internal/ssh/server.go" {
		t.Errorf("changed = %v, want [internal/ssh/server.go]", changed)
	}
	// A glob matching nothing touched comes back empty.
	none, err := gitDiffGlobs(context.Background(), bare, baseSHA, "HEAD", []string{"docs/**"})
	if err != nil {
		t.Fatalf("gitDiffGlobs docs: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("docs changed = %v, want none", none)
	}
}
