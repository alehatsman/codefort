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
