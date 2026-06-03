package server

import (
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
	"github.com/alehatsman/moongit/internal/dex"
	"github.com/alehatsman/moongit/internal/storage"
)

// fakeDexHit is one hit in the fake dex /search/semantic response.
type fakeDexHit struct {
	Path      string  `json:"path"`
	Kind      string  `json:"kind"`
	StartLine int     `json:"start_line"`
	EndLine   int     `json:"end_line"`
	Score     float32 `json:"score"`
	Content   string  `json:"content"`
}

// newSpecsSearchServer stands up a Server wired to a fake dex daemon whose
// semantic search returns the given hits, backed by a bare repo seeded with two
// specs (so section attribution reads real files). indexed=false makes dex
// report no matching project, exercising the un-indexed 404 path.
func newSpecsSearchServer(t *testing.T, hits []fakeDexHit, indexed bool) *Server {
	t.Helper()
	dexSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/status":
			projects := []map[string]any{}
			if indexed {
				projects = append(projects, map[string]any{"id": "pid", "root": "/repos/" + cRepo})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"reachable": true, "projects": projects})
		case filepath.Base(r.URL.Path) == "semantic":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "hits": hits})
		default:
			http.Error(w, "unexpected: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(dexSrv.Close)

	db, err := storage.Open(filepath.Join(t.TempDir(), "c.db"))
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
	// Line map for ssh-transport.md: 5 "# SSH Transport", 6 "## Intent",
	// 7 "Git over SSH.", 8 "## Behavior", 9 "WHEN pushed THEN verify."
	write("specs/ssh-transport.md", "---\nid: ssh-transport\nstatus: living\n---\n# SSH Transport\n## Intent\nGit over SSH.\n## Behavior\nWHEN pushed THEN verify.\n")
	write("specs/ci/pipeline.md", "# Pipeline\n## Intent\nThe CI DAG.\n")
	write("internal/ssh/server.go", "package ssh\n")
	git("add", ".")
	git("commit", "-q", "-m", "seed")
	bare := filepath.Join(reposDir, cOwner, cRepo+".git")
	if err := os.MkdirAll(filepath.Dir(bare), 0o755); err != nil {
		t.Fatal(err)
	}
	git("clone", "-q", "--bare", work, bare)

	return &Server{
		cfg:    &config.Config{ReposDir: reposDir},
		db:     db,
		rdb:    db,
		dex:    dex.New(dexSrv.URL, ""),
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func postSearch(t *testing.T, s *Server, body string) (api.SpecSearchResult, int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/repos/alice/proj/specs/search", strings.NewReader(body))
	req.SetPathValue("owner", cOwner)
	req.SetPathValue("repo", cRepo)
	rr := httptest.NewRecorder()
	s.handleSearchSpecs(rr, req)
	var out api.SpecSearchResult
	if rr.Code == http.StatusOK {
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v (body=%s)", err, rr.Body.String())
		}
	}
	return out, rr.Code
}

func TestHandleSearchSpecs(t *testing.T) {
	hits := []fakeDexHit{
		{Path: "specs/ssh-transport.md", Kind: "chunk", StartLine: 7, Score: 0.92, Content: "Git over SSH."},
		// A code hit — must be filtered out of the spec corpus.
		{Path: "internal/ssh/server.go", Kind: "chunk", StartLine: 1, Score: 0.81, Content: "package ssh"},
		{Path: "specs/ci/pipeline.md", Kind: "chunk", StartLine: 3, Score: 0.70, Content: "The CI DAG."},
	}
	s := newSpecsSearchServer(t, hits, true)

	out, code := postSearch(t, s, `{"query":"ssh"}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if out.Query != "ssh" {
		t.Errorf("query = %q", out.Query)
	}
	// Two specs/ hits; the code hit is dropped.
	if len(out.Hits) != 2 {
		t.Fatalf("got %d hits, want 2: %+v", len(out.Hits), out.Hits)
	}
	h0 := out.Hits[0]
	if h0.Path != "specs/ssh-transport.md" || h0.Line != 7 {
		t.Errorf("hit0 path/line = %q/%d", h0.Path, h0.Line)
	}
	// Line 7 falls under the "## Intent" heading (line 6).
	if h0.Section != "Intent" {
		t.Errorf("hit0 section = %q, want Intent", h0.Section)
	}
	if h0.Snippet != "Git over SSH." || h0.Score != 0.92 {
		t.Errorf("hit0 snippet/score = %q/%v", h0.Snippet, h0.Score)
	}
	if out.Hits[1].Path != "specs/ci/pipeline.md" || out.Hits[1].Section != "Intent" {
		t.Errorf("hit1 = %+v", out.Hits[1])
	}
	for _, h := range out.Hits {
		if !strings.HasPrefix(h.Path, "specs/") {
			t.Errorf("non-spec hit leaked: %q", h.Path)
		}
	}
}

func TestHandleSearchSpecsValidation(t *testing.T) {
	s := newSpecsSearchServer(t, nil, true)

	if _, code := postSearch(t, s, `{"query":"  "}`); code != http.StatusBadRequest {
		t.Errorf("blank query: status = %d, want 400", code)
	}
	if _, code := postSearch(t, s, `not json`); code != http.StatusBadRequest {
		t.Errorf("bad JSON: status = %d, want 400", code)
	}
}

func TestHandleSearchSpecsNotIndexed(t *testing.T) {
	s := newSpecsSearchServer(t, nil, false)
	if _, code := postSearch(t, s, `{"query":"ssh"}`); code != http.StatusNotFound {
		t.Errorf("un-indexed: status = %d, want 404", code)
	}
}

func TestHandleSearchSpecsDexDisabled(t *testing.T) {
	// A nil dex client is the unconfigured state (MOONGIT_DEX_URL unset) → 503.
	s := newSpecsSearchServer(t, nil, true)
	s.dex = nil
	if _, code := postSearch(t, s, `{"query":"ssh"}`); code != http.StatusServiceUnavailable {
		t.Errorf("dex disabled: status = %d, want 503", code)
	}
}
