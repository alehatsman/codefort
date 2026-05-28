package server

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/alehatsman/moongit/internal/api"
	"github.com/alehatsman/moongit/internal/storage"
)

func (s *Server) handleListRepos(w http.ResponseWriter, _ *http.Request) {
	rows, err := storage.ListRepos(s.db)
	if err != nil {
		s.logger.Error("list repos", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]api.Repo, 0, len(rows))
	for _, r := range rows {
		out = append(out, toAPIRepo(r))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetRepo(w http.ResponseWriter, r *http.Request) {
	owner := r.PathValue("owner")
	repo := strings.TrimSuffix(r.PathValue("repo"), ".git")

	row, err := storage.GetRepoSummary(s.db, owner, repo)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "repo not registered: "+owner+"/"+repo)
		return
	}
	if err != nil {
		s.logger.Error("get repo", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toAPIRepo(row))
}

func toAPIRepo(r storage.RepoSummary) api.Repo {
	return api.Repo{
		ID:          r.ID,
		Owner:       r.Owner,
		Name:        r.Name,
		CreatedAt:   time.Unix(r.CreatedAt, 0).UTC(),
		OpenIssues:  r.OpenIssues,
		TotalIssues: r.TotalIssues,
	}
}
