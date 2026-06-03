package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/alehatsman/moongit/internal/api"
	"github.com/alehatsman/moongit/internal/storage"
)

// The cross-repo aggregate endpoints back the fleet-wide list views (the
// top-level Issues / Pull requests / Pipelines / Agents tabs). They mirror the
// per-repo list endpoints' query params but drop the {owner}/{repo} scope and
// tag every row with its owning repo so the UI can link back to it.

// handleListAllIssues returns issues across every repo, newest-updated first.
// Same state/assignee/author/q/limit params as the per-repo list; sort is
// ignored (the cross-repo feed is always by recency — see storage.ListAllIssues).
func (s *Server) handleListAllIssues(w http.ResponseWriter, r *http.Request) {
	filter, err := parseListFilter(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	issues, err := storage.ListAllIssues(s.rdb, filter)
	if err != nil {
		s.logger.Error("list all issues", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	total, err := storage.CountAllIssues(s.rdb, filter)
	if err != nil {
		s.logger.Error("count all issues", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(total))
	writeJSON(w, http.StatusOK, issues)
}

// handleListAllPulls returns pull requests across every repo, newest-updated
// first. Same ?state= and ?q= (title/body keyword) filters as the per-repo list.
func (s *Server) handleListAllPulls(w http.ResponseWriter, r *http.Request) {
	states, err := parsePRStates(r.URL.Query()["state"])
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	pulls, err := storage.ListAllPulls(s.rdb, states, query)
	if err != nil {
		s.logger.Error("list all pulls", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, pulls)
}

// handleListAllRuns returns CI/agent runs across every repo, newest-created
// first. Filtered by the shared ?kind/?state/?q/?limit params (see
// parseRunFilter) so the fleet-wide Pipelines and Agents tabs each show only
// their own and can search/filter; the cap is enforced by storage.ListAllRuns.
func (s *Server) handleListAllRuns(w http.ResponseWriter, r *http.Request) {
	filter, ok := parseRunFilter(w, r)
	if !ok {
		return
	}

	runs, err := storage.ListAllRuns(s.rdb, filter)
	if err != nil {
		s.logger.Error("list all runs", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]api.CIRunWithRepo, 0, len(runs))
	for _, rr := range runs {
		out = append(out, api.CIRunWithRepo{
			CIRun: toAPIRun(rr.Run),
			Repo:  api.RepoRef{Owner: rr.Owner, Name: rr.Name},
		})
	}
	writeJSON(w, http.StatusOK, out)
}
