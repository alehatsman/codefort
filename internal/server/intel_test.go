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

// fakeDexChunk mirrors the fields dex's enumerate-summaries endpoint returns.
type fakeDexChunk struct {
	Path    string `json:"path"`
	Kind    string `json:"kind"`
	Content string `json:"content"`
}

// newSummariesServer stands up a Server wired to a fake dex daemon whose
// /v1/projects/{id}/summaries returns the given chunks. The fake answers
// /v1/status with one project (Root base == cRepo) so ResolveProject matches.
func newSummariesServer(t *testing.T, chunks []fakeDexChunk) *Server {
	t.Helper()
	dexSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/status":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"reachable": true,
				"projects":  []map[string]any{{"id": "pid", "root": "/repos/" + cRepo}},
			})
		case filepath.Base(r.URL.Path) == "summaries":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "summaries": chunks})
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

func getSummaries(t *testing.T, s *Server) (map[string]string, int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/repos/alice/proj/intel/summaries", nil)
	req.SetPathValue("owner", cOwner)
	req.SetPathValue("repo", cRepo)
	rr := httptest.NewRecorder()
	s.handleIntelSummaries(rr, req)
	var out summariesResponse
	if rr.Code == http.StatusOK {
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v (body=%s)", err, rr.Body.String())
		}
	}
	return out.Summaries, rr.Code
}

// The enumerated chunks become a flat path→prose map: repo at "", dirs and
// files by their path. The repo_summary (dex path ".") folds onto "".
func TestIntelSummaries(t *testing.T) {
	got, code := getSummaries(t, newSummariesServer(t, []fakeDexChunk{
		{Path: ".", Kind: "repo_summary", Content: "Repo overview."},
		{Path: "internal", Kind: "package_summary", Content: "Internal."},
		{Path: "internal/server", Kind: "package_summary", Content: "Server package."},
		{Path: "internal/server/tree.go", Kind: "file_summary", Content: "Tree file."},
		{Path: "", Kind: "file_summary", Content: ""}, // empty content dropped
	}))
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	want := map[string]string{
		"":                        "Repo overview.",
		"internal":                "Internal.",
		"internal/server":         "Server package.",
		"internal/server/tree.go": "Tree file.",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries %v, want %d", len(got), got, len(want))
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("summaries[%q] = %q, want %q", k, got[k], v)
		}
	}
}

// When dex has no dedicated repo_summary, the repo-root package summary
// (path ".") fills the "" slot instead.
func TestIntelSummariesRepoRootPackageFallback(t *testing.T) {
	got, _ := getSummaries(t, newSummariesServer(t, []fakeDexChunk{
		{Path: ".", Kind: "package_summary", Content: "Root package."},
		{Path: "cmd", Kind: "package_summary", Content: "Commands."},
	}))
	if got[""] != "Root package." {
		t.Errorf("repo slot = %q, want %q", got[""], "Root package.")
	}
	if _, ok := got["."]; ok {
		t.Errorf(`"." should not appear as its own key, got %q`, got["."])
	}
}

// handleIntelPackageGraph resolves the repo to its dex project and passes
// dex's /graph/packages body straight through to the browser.
func TestIntelPackageGraph(t *testing.T) {
	const graph = `{"status":"ok",
		"nodes":[{"package":"mod/store","in_degree":5,"out_degree":1,"page_rank":0.05}],
		"edges":[{"from_package":"mod/cmd","to_package":"mod/store"}]}`
	dexSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/status":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"reachable": true,
				"projects":  []map[string]any{{"id": "pid", "root": "/repos/" + cRepo}},
			})
		case filepath.Base(r.URL.Path) == "packages":
			_, _ = w.Write([]byte(graph))
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
	s := &Server{
		cfg:    &config.Config{},
		db:     db,
		rdb:    db,
		dex:    dex.New(dexSrv.URL, ""),
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/repos/alice/proj/intel/package-graph", nil)
	req.SetPathValue("owner", cOwner)
	req.SetPathValue("repo", cRepo)
	rr := httptest.NewRecorder()
	s.handleIntelPackageGraph(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
	var out dex.PackageGraph
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, rr.Body.String())
	}
	if out.Status != "ok" || len(out.Nodes) != 1 || out.Nodes[0].Package != "mod/store" || out.Nodes[0].InDegree != 5 {
		t.Errorf("unexpected passthrough: %+v", out)
	}
	if len(out.Edges) != 1 || out.Edges[0].FromPackage != "mod/cmd" {
		t.Errorf("edges not passed through: %+v", out.Edges)
	}
}
