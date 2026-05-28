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

func (s *Server) handleListComments(w http.ResponseWriter, r *http.Request) {
	issueID, ok := s.lookupIssueOrFail(w, r)
	if !ok {
		return
	}
	comments, err := storage.ListComments(s.db, issueID)
	if err != nil {
		s.logger.Error("list comments", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, comments)
}

func (s *Server) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	issueID, ok := s.lookupIssueOrFail(w, r)
	if !ok {
		return
	}

	var req api.CreateCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	req.Author = strings.TrimSpace(req.Author)
	req.Body = strings.TrimSpace(req.Body)
	if req.Author == "" {
		writeError(w, http.StatusBadRequest, "author is required")
		return
	}
	if req.Body == "" {
		writeError(w, http.StatusBadRequest, "body is required")
		return
	}

	c, err := storage.CreateComment(s.db, issueID, req)
	if err != nil {
		s.logger.Error("create comment", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

// lookupIssueOrFail resolves {owner}/{repo}/issues/{number} to the issue's
// internal row id, writing an HTTP error and returning ok=false on failure.
func (s *Server) lookupIssueOrFail(w http.ResponseWriter, r *http.Request) (int64, bool) {
	num, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || num <= 0 {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return 0, false
	}
	repoID, ok := s.lookupRepoOrFail(w, r)
	if !ok {
		return 0, false
	}
	iss, err := storage.GetIssue(s.db, repoID, num)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "issue not found")
		return 0, false
	}
	if err != nil {
		s.logger.Error("get issue", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return 0, false
	}
	return iss.ID, true
}
