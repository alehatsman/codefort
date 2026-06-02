package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/alehatsman/moongit/internal/dex"
)

// intelResponse is the payload behind the Intel tab: dex index status for
// the repo, plus enough about the dex service to render a helpful empty
// state when nothing is indexed or dex isn't configured.
type intelResponse struct {
	Enabled bool          `json:"enabled"`           // dex configured at all
	Found   bool          `json:"found"`             // a matching indexed project exists
	Service *intelService `json:"service,omitempty"` // dex daemon status
	Project *intelProject `json:"project,omitempty"` // the matched project's index stats
}

type intelService struct {
	Endpoint  string `json:"endpoint"`
	Reachable bool   `json:"reachable"`
	Model     string `json:"model"`
	Version   string `json:"version"`
}

type intelProject struct {
	Root             string `json:"root"`
	Chunks           int    `json:"chunks"`
	Files            int    `json:"files"`
	Dim              int    `json:"dim"`
	EmbedModel       string `json:"embed_model"`
	LastIndexed      string `json:"last_indexed"`
	PendingSummaries int    `json:"pending_summaries"`
}

// handleIntel returns dex index status for the repo. It never fails the
// request just because dex is down or the repo isn't indexed — those are
// expected states the UI renders distinctly (enabled/found flags). Only a
// missing repo (404) or an outright dex transport error (502) are errors.
func (s *Server) handleIntel(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.lookupRepoOrFail(w, r); !ok {
		return
	}
	repo := strings.TrimSuffix(r.PathValue("repo"), ".git")

	if !s.dex.Enabled() {
		writeJSON(w, http.StatusOK, intelResponse{Enabled: false})
		return
	}

	st, err := s.dex.Status(r.Context())
	if err != nil {
		s.logger.Error("dex status", "err", err)
		writeError(w, http.StatusBadGateway, "dex unreachable: "+err.Error())
		return
	}

	resp := intelResponse{
		Enabled: true,
		Service: &intelService{
			Endpoint:  st.Endpoint,
			Reachable: st.Reachable,
			Model:     st.Model,
			Version:   st.Version,
		},
	}
	for _, p := range st.Projects {
		if strings.EqualFold(filepath.Base(p.Root), repo) {
			resp.Found = true
			resp.Project = &intelProject{
				Root:             p.Root,
				Chunks:           p.Chunks,
				Files:            p.Files,
				Dim:              p.Dim,
				EmbedModel:       p.EmbedModel,
				LastIndexed:      p.LastIndexed,
				PendingSummaries: p.PendingSummaries,
			}
			break
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

type intelSearchRequest struct {
	Query string `json:"query"`
	Kind  string `json:"kind"` // "semantic" (default) | "symbol"
}

// handleIntelOverview returns the at-a-glance repo + package summaries
// dex composed at index time. Used by the Intel tab to render an overview
// before the user has typed a query.
func (s *Server) handleIntelOverview(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.lookupRepoOrFail(w, r); !ok {
		return
	}
	repo := strings.TrimSuffix(r.PathValue("repo"), ".git")
	if !s.dex.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "dex integration not configured")
		return
	}
	proj, err := s.dex.ResolveProject(r.Context(), repo)
	if errors.Is(err, dex.ErrProjectNotFound) {
		writeError(w, http.StatusNotFound, "repo is not indexed by dex")
		return
	}
	if err != nil {
		s.logger.Error("dex resolve project", "err", err)
		writeError(w, http.StatusBadGateway, "dex unreachable: "+err.Error())
		return
	}
	ov, err := s.dex.Overview(r.Context(), proj.ID)
	if err != nil {
		s.logger.Error("dex overview", "err", err)
		writeError(w, http.StatusBadGateway, "dex overview failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ov)
}

// handleIntelPackageGraph returns the internal package import DAG dex
// computed for the repo, backing the Explore "Map of the codebase" so it
// can rank and layer packages by real import structure. A non-Go or
// un-graphed project comes back as a 200 with status "no-graph" and no
// nodes (not an error) — the UI degrades to its flat package listing.
func (s *Server) handleIntelPackageGraph(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.lookupRepoOrFail(w, r); !ok {
		return
	}
	repo := strings.TrimSuffix(r.PathValue("repo"), ".git")
	if !s.dex.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "dex integration not configured")
		return
	}
	proj, err := s.dex.ResolveProject(r.Context(), repo)
	if errors.Is(err, dex.ErrProjectNotFound) {
		writeError(w, http.StatusNotFound, "repo is not indexed by dex")
		return
	}
	if err != nil {
		s.logger.Error("dex resolve project", "err", err)
		writeError(w, http.StatusBadGateway, "dex unreachable: "+err.Error())
		return
	}
	pg, err := s.dex.PackageGraph(r.Context(), proj.ID)
	if err != nil {
		s.logger.Error("dex package graph", "err", err)
		writeError(w, http.StatusBadGateway, "dex package graph failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, pg)
}

// fileSummaryResponse is the payload for the per-file dex summary surfaced
// on the Code tab's blob view. Summary is empty when dex has no summary for
// the file (the common case — only some files are summarized), which the UI
// renders as a plain breadcrumb with no card.
type fileSummaryResponse struct {
	Path    string `json:"path"`
	Summary string `json:"summary"`
}

// handleIntelFileSummary returns the dex file_summary chunk for a single
// file (?path=...), backing the collapsible overview card on the blob view.
// A missing summary is a 200 with an empty summary, not an error — most
// files simply aren't summarized.
func (s *Server) handleIntelFileSummary(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.lookupRepoOrFail(w, r); !ok {
		return
	}
	repo := strings.TrimSuffix(r.PathValue("repo"), ".git")
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	if !s.dex.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "dex integration not configured")
		return
	}
	proj, err := s.dex.ResolveProject(r.Context(), repo)
	if errors.Is(err, dex.ErrProjectNotFound) {
		writeError(w, http.StatusNotFound, "repo is not indexed by dex")
		return
	}
	if err != nil {
		s.logger.Error("dex resolve project", "err", err)
		writeError(w, http.StatusBadGateway, "dex unreachable: "+err.Error())
		return
	}
	summary, err := s.dex.FileSummary(r.Context(), proj.ID, path)
	if err != nil {
		s.logger.Error("dex file summary", "err", err)
		writeError(w, http.StatusBadGateway, "dex file summary failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, fileSummaryResponse{Path: path, Summary: summary})
}

// summariesResponse is a flat map from repo sub-path to its dex summary: ""
// is the repo, directory paths carry their package summary, file paths their
// file summary. The UI consumes one map per repo for both the breadcrumb
// (filter to ancestor sub-paths) and the file tree (look up each entry).
type summariesResponse struct {
	Summaries map[string]string `json:"summaries"`
}

// handleIntelSummaries returns every summary dex composed for the repo as one
// path→prose map, enumerated in a single dex call. Reliable and complete
// (unlike per-path semantic recall), and cached client-side per repo. dex's
// repo summary arrives under path "." — folded onto "" here. Paths dex has no
// prose for are simply absent, so the UI leaves those crumbs/rows plain.
func (s *Server) handleIntelSummaries(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.lookupRepoOrFail(w, r); !ok {
		return
	}
	repo := strings.TrimSuffix(r.PathValue("repo"), ".git")

	if !s.dex.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "dex integration not configured")
		return
	}
	proj, err := s.dex.ResolveProject(r.Context(), repo)
	if errors.Is(err, dex.ErrProjectNotFound) {
		writeError(w, http.StatusNotFound, "repo is not indexed by dex")
		return
	}
	if err != nil {
		s.logger.Error("dex resolve project", "err", err)
		writeError(w, http.StatusBadGateway, "dex unreachable: "+err.Error())
		return
	}

	chunks, err := s.dex.AllSummaries(r.Context(), proj.ID)
	if err != nil {
		s.logger.Error("dex summaries", "err", err)
		writeError(w, http.StatusBadGateway, "dex summaries failed: "+err.Error())
		return
	}

	out := map[string]string{}
	for _, ch := range chunks {
		if ch.Content == "" {
			continue
		}
		switch ch.Kind {
		case "repo_summary":
			out[""] = ch.Content
		case "package_summary":
			if ch.Path == "." {
				// Repo-root package: fall back onto "" only if dex had no
				// dedicated repo_summary.
				if _, ok := out[""]; !ok {
					out[""] = ch.Content
				}
				continue
			}
			out[ch.Path] = ch.Content
		case "file_summary":
			out[ch.Path] = ch.Content
		}
	}
	writeJSON(w, http.StatusOK, summariesResponse{Summaries: out})
}

// handleIntelSearch proxies a semantic or symbol search to dex, scoped to
// the dex project that matches this repo.
func (s *Server) handleIntelSearch(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.lookupRepoOrFail(w, r); !ok {
		return
	}
	repo := strings.TrimSuffix(r.PathValue("repo"), ".git")

	if !s.dex.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "dex integration not configured")
		return
	}

	var req intelSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	req.Query = strings.TrimSpace(req.Query)
	if req.Query == "" {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}

	proj, err := s.dex.ResolveProject(r.Context(), repo)
	if errors.Is(err, dex.ErrProjectNotFound) {
		writeError(w, http.StatusNotFound, "repo is not indexed by dex")
		return
	}
	if err != nil {
		s.logger.Error("dex resolve project", "err", err)
		writeError(w, http.StatusBadGateway, "dex unreachable: "+err.Error())
		return
	}

	const maxHits = 20
	var res *dex.SearchResult
	switch req.Kind {
	case "symbol":
		res, err = s.dex.FindSymbol(r.Context(), proj.ID, req.Query, maxHits)
	case "ask":
		res, err = s.dex.Ask(r.Context(), proj.ID, req.Query, maxHits)
	case "callers":
		res, err = s.dex.Callers(r.Context(), proj.ID, req.Query, maxHits)
	case "callees":
		res, err = s.dex.Callees(r.Context(), proj.ID, req.Query, maxHits)
	default:
		res, err = s.dex.Search(r.Context(), proj.ID, req.Query, maxHits)
	}
	if err != nil {
		s.logger.Error("dex search", "err", err, "kind", req.Kind)
		writeError(w, http.StatusBadGateway, "dex search failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}
