package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"sync"

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

// pathSummariesResponse maps each breadcrumb sub-path to its dex summary:
// "" → the repo summary, directory paths → package summaries, and the leaf
// file path → its file summary. Only sub-paths dex actually has prose for
// appear; the rest are omitted so the UI leaves those crumbs plain.
type pathSummariesResponse struct {
	Summaries map[string]string `json:"summaries"`
}

// handleIntelPathSummaries returns, for a repo path, the dex summary of every
// breadcrumb segment up to it: the repo, each ancestor directory, and — when
// ?file=1 (the caller is on a blob) — the leaf file. Each is recalled by a
// path-keyed search (see dex.PathSummary), reliable where the broad Overview
// enumeration drops package summaries. The work is bounded by path depth, not
// repo size, and the lookups run concurrently. Backs the Code tab's
// per-segment hover tooltips. A path dex has nothing for is omitted, not an
// error — the common case for non-package directories.
func (s *Server) handleIntelPathSummaries(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.lookupRepoOrFail(w, r); !ok {
		return
	}
	repo := strings.TrimSuffix(r.PathValue("repo"), ".git")
	path := strings.Trim(strings.TrimSpace(r.URL.Query().Get("path")), "/")
	leafIsFile := r.URL.Query().Get("file") == "1"

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

	// Assemble the lookups: repo root, then each ancestor directory as a
	// package, with the leaf treated as a file when the caller is on a blob.
	type lookup struct{ subPath, kind string }
	lookups := []lookup{{subPath: "", kind: "repo_summary"}}
	if path != "" {
		segs := strings.Split(path, "/")
		for i := range segs {
			kind := "package_summary"
			if leafIsFile && i == len(segs)-1 {
				kind = "file_summary"
			}
			lookups = append(lookups, lookup{subPath: strings.Join(segs[:i+1], "/"), kind: kind})
		}
	}

	// Each lookup is an independent dex round trip and the chain is short
	// (path depth), so fan out a goroutine apiece to keep latency flat.
	summaries := make([]string, len(lookups))
	errs := make([]error, len(lookups))
	var wg sync.WaitGroup
	for i, lk := range lookups {
		wg.Add(1)
		go func(i int, lk lookup) {
			defer wg.Done()
			if lk.kind == "repo_summary" {
				summaries[i], errs[i] = s.dex.RepoSummary(r.Context(), proj.ID)
				return
			}
			summaries[i], errs[i] = s.dex.PathSummary(r.Context(), proj.ID, lk.subPath, lk.kind)
		}(i, lk)
	}
	wg.Wait()

	out := map[string]string{}
	for i, lk := range lookups {
		if errs[i] != nil {
			s.logger.Error("dex path summary", "err", errs[i], "path", lk.subPath)
			writeError(w, http.StatusBadGateway, "dex path summary failed: "+errs[i].Error())
			return
		}
		if summaries[i] != "" {
			out[lk.subPath] = summaries[i]
		}
	}
	writeJSON(w, http.StatusOK, pathSummariesResponse{Summaries: out})
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
