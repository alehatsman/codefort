package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/alehatsman/moongit/internal/config"
	"github.com/alehatsman/moongit/internal/dex"
	"github.com/alehatsman/moongit/internal/storage"
)

// fakeDexHit mirrors the fields dex.Hit carries that our handlers read.
type fakeDexHit struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Content string `json:"content"`
}

// newPathSummaryServer stands up a Server wired to a fake dex daemon. The
// fake answers /v1/status with one project (its Root base == cRepo, so
// ResolveProject matches) and returns the given hits for every semantic
// search — handleIntelPathSummaries selects per (path, kind) itself, so a
// sub-path with no matching hit is correctly omitted.
func newPathSummaryServer(t *testing.T, hits []fakeDexHit) *Server {
	t.Helper()
	dexSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/status":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"reachable": true,
				"projects": []map[string]any{
					{"id": "pid", "root": "/repos/" + cRepo},
				},
			})
		case r.Method == http.MethodPost && filepath.Base(r.URL.Path) == "semantic":
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

	return &Server{
		cfg:    &config.Config{},
		db:     db,
		rdb:    db,
		dex:    dex.New(dexSrv.URL, ""),
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func getPathSummaries(t *testing.T, s *Server, target string) (map[string]string, int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.SetPathValue("owner", cOwner)
	req.SetPathValue("repo", cRepo)
	rr := httptest.NewRecorder()
	s.handleIntelPathSummaries(rr, req)
	var out pathSummariesResponse
	if rr.Code == http.StatusOK {
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode %s: %v (body=%s)", target, err, rr.Body.String())
		}
	}
	return out.Summaries, rr.Code
}

// On a blob, every ancestor directory that dex summarized as a package gets
// its package_summary, the repo gets its repo_summary, and the leaf file gets
// its file_summary. A directory dex has no package summary for (here
// "internal") is omitted, not faked.
func TestPathSummariesBlob(t *testing.T) {
	s := newPathSummaryServer(t, []fakeDexHit{
		{Path: "", Kind: "repo_summary", Content: "Repo overview."},
		{Path: "internal/server", Kind: "package_summary", Content: "Server package."},
		{Path: "internal/server/tree.go", Kind: "file_summary", Content: "Tree file."},
		// A decoy: same path, wrong kind — must not be picked for the dir crumb.
		{Path: "internal/server", Kind: "file_summary", Content: "WRONG"},
	})

	got, code := getPathSummaries(t, s, "/api/repos/alice/proj/intel/path-summaries?path=internal/server/tree.go&file=1")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	want := map[string]string{
		"":                        "Repo overview.",
		"internal/server":         "Server package.",
		"internal/server/tree.go": "Tree file.",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries %v, want %d %v", len(got), got, len(want), want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("summaries[%q] = %q, want %q", k, got[k], v)
		}
	}
	if _, ok := got["internal"]; ok {
		t.Errorf("non-package dir %q should be omitted, got %q", "internal", got["internal"])
	}
}

// On a tree (dir) view there is no file param, so the leaf is treated as a
// directory and looked up as a package — never as a file.
func TestPathSummariesTreeLeafIsPackage(t *testing.T) {
	s := newPathSummaryServer(t, []fakeDexHit{
		{Path: "", Kind: "repo_summary", Content: "Repo overview."},
		{Path: "internal/server", Kind: "package_summary", Content: "Server package."},
		// If the leaf were (incorrectly) treated as a file, this would surface.
		{Path: "internal/server", Kind: "file_summary", Content: "WRONG"},
	})

	got, code := getPathSummaries(t, s, "/api/repos/alice/proj/intel/path-summaries?path=internal/server")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if got["internal/server"] != "Server package." {
		t.Errorf("leaf dir summary = %q, want %q", got["internal/server"], "Server package.")
	}
}

// At the repo root the response carries just the repo summary.
func TestPathSummariesRoot(t *testing.T) {
	s := newPathSummaryServer(t, []fakeDexHit{
		{Path: "", Kind: "repo_summary", Content: "Repo overview."},
	})
	got, code := getPathSummaries(t, s, "/api/repos/alice/proj/intel/path-summaries?path=")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if len(got) != 1 || got[""] != "Repo overview." {
		t.Fatalf("root summaries = %v, want {\"\": \"Repo overview.\"}", got)
	}
}
