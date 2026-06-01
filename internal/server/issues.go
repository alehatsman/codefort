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
	repoID, ok := s.lookupRepoOrFail(w, r)
	if !ok {
		return
	}

	var req api.CreateIssueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	// Identity is stamped from the authenticated token; client-supplied
	// author is ignored.
	req.Author = identityFromContext(r)

	iss, err := storage.CreateIssue(s.db, repoID, req)
	if err != nil {
		s.logger.Error("create issue", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.emit("issue.created", repoID, iss.Author, map[string]any{"number": iss.Number, "title": iss.Title})
	writeJSON(w, http.StatusCreated, iss)
}

func (s *Server) handleListIssues(w http.ResponseWriter, r *http.Request) {
	repoID, ok := s.lookupRepoOrFail(w, r)
	if !ok {
		return
	}

	filter, err := parseListFilter(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	issues, err := storage.ListIssues(s.rdb, repoID, filter)
	if err != nil {
		s.logger.Error("list issues", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, issues)
}

func (s *Server) handleGetIssue(w http.ResponseWriter, r *http.Request) {
	num, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || num <= 0 {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}

	repoID, ok := s.lookupRepoOrFail(w, r)
	if !ok {
		return
	}

	iss, err := storage.GetIssue(s.rdb, repoID, num)
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

func (s *Server) handleUpdateIssue(w http.ResponseWriter, r *http.Request) {
	num, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || num <= 0 {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}

	var req api.UpdateIssueRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.State != nil && !req.State.Valid() {
		writeError(w, http.StatusBadRequest, "invalid state: "+string(*req.State))
		return
	}
	// Title is NOT NULL and meaningful — reject blanking it. Trim in place
	// so the stored value matches create's behavior.
	if req.Title != nil {
		trimmed := strings.TrimSpace(*req.Title)
		if trimmed == "" {
			writeError(w, http.StatusBadRequest, "title cannot be empty")
			return
		}
		req.Title = &trimmed
	}

	repoID, ok := s.lookupRepoOrFail(w, r)
	if !ok {
		return
	}

	iss, err := storage.UpdateIssue(s.db, repoID, num, req.State, req.Title, req.Body)
	if errors.Is(err, storage.ErrNoUpdateFields) {
		writeError(w, http.StatusBadRequest, "no fields to update (provide state, title, and/or body)")
		return
	}
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	if err != nil {
		s.logger.Error("update issue", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// A state transition is the fleet-relevant signal (todo→in_progress→done);
	// a title/body edit is a plainer "updated".
	actor := identityFromContext(r)
	if req.State != nil {
		s.emit("issue.state_changed", repoID, actor, map[string]any{"number": iss.Number, "state": string(iss.State)})
	} else {
		s.emit("issue.updated", repoID, actor, map[string]any{"number": iss.Number})
	}
	writeJSON(w, http.StatusOK, iss)
}

func (s *Server) handleDeleteIssue(w http.ResponseWriter, r *http.Request) {
	num, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || num <= 0 {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}

	repoID, ok := s.lookupRepoOrFail(w, r)
	if !ok {
		return
	}

	// Data plane is intentionally open: any valid token may delete, matching
	// the posture of create/update/claim. Auth (a valid token) is enforced by
	// the /api withAuth middleware.
	err = storage.DeleteIssue(s.db, repoID, num)
	switch {
	case errors.Is(err, storage.ErrNotFound):
		writeError(w, http.StatusNotFound, "issue not found")
	case err != nil:
		s.logger.Error("delete issue", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleClaimIssue(w http.ResponseWriter, r *http.Request) {
	num, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || num <= 0 {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}

	var req api.ClaimRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	// Identity is stamped from the authenticated token; client-supplied
	// assignee is ignored.
	req.Assignee = identityFromContext(r)
	if req.State != "" && !req.State.Valid() {
		writeError(w, http.StatusBadRequest, "invalid state: "+string(req.State))
		return
	}

	repoID, ok := s.lookupRepoOrFail(w, r)
	if !ok {
		return
	}

	iss, err := storage.Claim(s.db, repoID, num, req.Assignee, req.State, s.cfg.ClaimLease)
	switch {
	case errors.Is(err, storage.ErrNotFound):
		writeError(w, http.StatusNotFound, "issue not found")
		return
	case errors.Is(err, storage.ErrAlreadyClaimed):
		writeError(w, http.StatusConflict, "issue already claimed")
		return
	case err != nil:
		s.logger.Error("claim issue", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.emit("issue.claimed", repoID, req.Assignee, map[string]any{"number": iss.Number, "state": string(iss.State)})
	writeJSON(w, http.StatusOK, iss)
}

func (s *Server) handleUnclaimIssue(w http.ResponseWriter, r *http.Request) {
	num, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || num <= 0 {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}

	repoID, ok := s.lookupRepoOrFail(w, r)
	if !ok {
		return
	}

	iss, err := storage.Unclaim(s.db, repoID, num, identityFromContext(r))
	switch {
	case errors.Is(err, storage.ErrNotFound):
		writeError(w, http.StatusNotFound, "issue not found")
		return
	case errors.Is(err, storage.ErrNotOwner):
		writeError(w, http.StatusForbidden, "issue claimed by another agent")
		return
	case err != nil:
		s.logger.Error("unclaim issue", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.emit("issue.unclaimed", repoID, identityFromContext(r), map[string]any{"number": iss.Number})
	writeJSON(w, http.StatusOK, iss)
}

// lookupRepoOrFail resolves the repo from the request's {owner}/{repo} path
// values, writing an appropriate HTTP error and returning ok=false on
// failure. Trailing ".git" on the repo segment is stripped.
func (s *Server) lookupRepoOrFail(w http.ResponseWriter, r *http.Request) (int64, bool) {
	owner := r.PathValue("owner")
	repo := strings.TrimSuffix(r.PathValue("repo"), ".git")

	repoID, err := storage.LookupRepo(s.rdb, owner, repo)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "repo not registered: "+owner+"/"+repo)
		return 0, false
	}
	if err != nil {
		s.logger.Error("lookup repo", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return 0, false
	}
	return repoID, true
}

// parseListFilter pulls list filters from URL query params. Returns a
// validated ListFilter or an error suitable for an HTTP 400 body.
func parseListFilter(q map[string][]string) (storage.ListFilter, error) {
	f := storage.ListFilter{}
	for _, raw := range q["state"] {
		for s := range strings.SplitSeq(raw, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			st := api.IssueState(s)
			if !st.Valid() {
				return f, errors.New("invalid state: " + s)
			}
			f.States = append(f.States, st)
		}
	}
	if v := q["assignee"]; len(v) > 0 {
		f.Assignee = v[0]
	}
	if v := q["author"]; len(v) > 0 {
		f.Author = v[0]
	}
	if v := q["q"]; len(v) > 0 {
		f.Query = strings.TrimSpace(v[0])
	}
	if v := q["sort"]; len(v) > 0 && v[0] != "" {
		s := api.IssueSort(v[0])
		if !s.Valid() {
			return f, errors.New("invalid sort: " + v[0])
		}
		f.Sort = s
	}
	if v := q["limit"]; len(v) > 0 {
		n, err := strconv.Atoi(v[0])
		if err != nil || n < 0 {
			return f, errors.New("invalid limit")
		}
		f.Limit = n
	}
	return f, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, api.ErrorResponse{Error: msg})
}
