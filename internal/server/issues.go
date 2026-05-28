package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/alehatsman/moongit/internal/api"
	"github.com/alehatsman/moongit/internal/storage"
)

func (s *Server) handleCreateIssue(w http.ResponseWriter, r *http.Request) {
	owner, repo := r.PathValue("owner"), normalizeRepo(r.PathValue("repo"))

	var req api.CreateIssueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	req.Author = strings.TrimSpace(req.Author)
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if req.Author == "" {
		writeError(w, http.StatusBadRequest, "author is required")
		return
	}

	repoID, err := storage.LookupRepo(s.db, owner, repo)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "repo not registered: "+owner+"/"+repo)
		return
	}
	if err != nil {
		s.logger.Error("lookup repo", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	iss, err := storage.CreateIssue(s.db, repoID, req)
	if err != nil {
		s.logger.Error("create issue", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, iss)
}

func (s *Server) handleListIssues(w http.ResponseWriter, r *http.Request) {
	owner, repo := r.PathValue("owner"), normalizeRepo(r.PathValue("repo"))

	repoID, err := storage.LookupRepo(s.db, owner, repo)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "repo not registered: "+owner+"/"+repo)
		return
	}
	if err != nil {
		s.logger.Error("lookup repo", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	issues, err := storage.ListIssues(s.db, repoID)
	if err != nil {
		s.logger.Error("list issues", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, issues)
}

func (s *Server) handleGetIssue(w http.ResponseWriter, r *http.Request) {
	owner, repo := r.PathValue("owner"), normalizeRepo(r.PathValue("repo"))

	num, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || num <= 0 {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}

	repoID, err := storage.LookupRepo(s.db, owner, repo)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "repo not registered: "+owner+"/"+repo)
		return
	}
	if err != nil {
		s.logger.Error("lookup repo", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	iss, err := storage.GetIssue(s.db, repoID, num)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	if err != nil {
		s.logger.Error("get issue", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, iss)
}

// normalizeRepo strips a trailing .git so the issues API accepts both
// "/api/v1/repos/aleh/hello/issues" and "/api/v1/repos/aleh/hello.git/issues".
func normalizeRepo(name string) string {
	return strings.TrimSuffix(name, ".git")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, api.ErrorResponse{Error: msg})
}
