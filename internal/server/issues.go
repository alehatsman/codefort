package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/alehatsman/codefort/internal/api"
	"github.com/alehatsman/codefort/internal/storage"
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

	if req.Parent != nil && *req.Parent < 0 {
		writeError(w, http.StatusBadRequest, "parent must be a positive issue number")
		return
	}

	iss, err := storage.CreateIssue(s.db, repoID, req)
	if errors.Is(err, storage.ErrInvalidInput) {
		writeError(w, http.StatusBadRequest, "parent issue not found in this repo")
		return
	}
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
	// The epics view carries a live child-completion rollup per umbrella, so the
	// list renders progress without an N+1 fetch.
	if filter.Epics && len(issues) > 0 {
		progress, err := storage.ChildProgress(s.rdb, repoID)
		if err != nil {
			s.logger.Error("epic progress", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		for i := range issues {
			if p, ok := progress[issues[i].Number]; ok {
				issues[i].Progress = &p
			}
		}
	}
	total, err := storage.CountIssues(s.rdb, repoID, filter)
	if err != nil {
		s.logger.Error("count issues", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(total))
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
	children, err := storage.ListChildren(s.rdb, repoID, num)
	if err != nil {
		s.logger.Error("list children", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if len(children) > 0 {
		iss.Children = children
		iss.Progress = progressFromChildren(children)
	}
	dependsOn, err := storage.ListDependencies(s.rdb, repoID, num)
	if err != nil {
		s.logger.Error("list dependencies", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if len(dependsOn) > 0 {
		iss.DependsOn = dependsOn
	}
	blocks, err := storage.ListDependents(s.rdb, repoID, num)
	if err != nil {
		s.logger.Error("list dependents", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if len(blocks) > 0 {
		iss.Blocks = blocks
	}
	writeJSON(w, http.StatusOK, iss)
}

// handleAddDependency records a depends-on edge: the path issue depends on the
// issue named in the request body. Rejects self-edges (400) and edges that
// would close a cycle (409). Both issues must exist in the repo (400 otherwise).
func (s *Server) handleAddDependency(w http.ResponseWriter, r *http.Request) {
	num, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || num <= 0 {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}

	var req api.AddDependencyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.DependsOn <= 0 {
		writeError(w, http.StatusBadRequest, "depends_on must be a positive issue number")
		return
	}

	repoID, ok := s.lookupRepoOrFail(w, r)
	if !ok {
		return
	}

	err = storage.AddDependency(s.db, repoID, num, req.DependsOn)
	switch {
	case errors.Is(err, storage.ErrSelfDependency):
		writeError(w, http.StatusBadRequest, "an issue cannot depend on itself")
		return
	case errors.Is(err, storage.ErrDependencyCycle):
		writeError(w, http.StatusConflict, "dependency would create a cycle")
		return
	case errors.Is(err, storage.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "issue not found in this repo")
		return
	case err != nil:
		s.logger.Error("add dependency", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.emit("issue.updated", repoID, identityFromContext(r), map[string]any{"number": num, "depends_on": req.DependsOn})
	s.writeIssueWithEdges(w, repoID, num)
}

// handleRemoveDependency deletes a depends-on edge. Removing a non-existent
// edge is an idempotent success.
func (s *Server) handleRemoveDependency(w http.ResponseWriter, r *http.Request) {
	num, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || num <= 0 {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}
	target, err := strconv.Atoi(r.PathValue("target"))
	if err != nil || target <= 0 {
		writeError(w, http.StatusBadRequest, "invalid dependency target")
		return
	}

	repoID, ok := s.lookupRepoOrFail(w, r)
	if !ok {
		return
	}

	if err := storage.RemoveDependency(s.db, repoID, num, target); err != nil {
		s.logger.Error("remove dependency", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.emit("issue.updated", repoID, identityFromContext(r), map[string]any{"number": num, "removed_depends_on": target})
	s.writeIssueWithEdges(w, repoID, num)
}

// writeIssueWithEdges fetches an issue and its edge sets (children, blocked-by,
// blocks) and writes it as the response. Shared by the dependency mutation
// handlers so an edge change returns the same shape as GET. A missing issue is
// reported as 404.
func (s *Server) writeIssueWithEdges(w http.ResponseWriter, repoID int64, num int) {
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
	if children, err := storage.ListChildren(s.rdb, repoID, num); err == nil && len(children) > 0 {
		iss.Children = children
		iss.Progress = progressFromChildren(children)
	}
	if deps, err := storage.ListDependencies(s.rdb, repoID, num); err == nil && len(deps) > 0 {
		iss.DependsOn = deps
	}
	if blocks, err := storage.ListDependents(s.rdb, repoID, num); err == nil && len(blocks) > 0 {
		iss.Blocks = blocks
	}
	writeJSON(w, http.StatusOK, iss)
}

// progressFromChildren computes an epic's completion rollup from its already
// fetched child summaries, so the single-issue view needs no extra query.
func progressFromChildren(children []api.ChildIssueSummary) *api.EpicProgress {
	p := api.EpicProgress{Total: len(children)}
	for _, c := range children {
		if c.State == api.IssueDone || c.State == api.IssueClosed {
			p.Done++
		}
	}
	return &p
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

	if req.Parent != nil && *req.Parent < 0 {
		writeError(w, http.StatusBadRequest, "parent must be a positive issue number or 0 to clear")
		return
	}

	iss, err := storage.UpdateIssue(s.db, repoID, num, req.State, req.Title, req.Body, req.Parent, req.Labels)
	if errors.Is(err, storage.ErrInvalidInput) {
		writeError(w, http.StatusBadRequest, "parent issue not found in this repo or is self-referential")
		return
	}
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
	if v := q["label"]; len(v) > 0 {
		f.Label = strings.TrimSpace(v[0])
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
	if v := q["offset"]; len(v) > 0 {
		n, err := strconv.Atoi(v[0])
		if err != nil || n < 0 {
			return f, errors.New("invalid offset")
		}
		f.Offset = n
	}
	f.Ready = queryBool(q, "ready")
	f.Blocked = queryBool(q, "blocked")
	f.Epics = queryBool(q, "epics")
	if f.Ready && f.Blocked {
		return f, errors.New("ready and blocked are mutually exclusive")
	}
	// epics selects umbrellas; ready/blocked exclude umbrellas — combining them
	// is always empty and signals confusion, so reject it.
	if f.Epics && (f.Ready || f.Blocked) {
		return f, errors.New("epics cannot be combined with ready or blocked")
	}
	return f, nil
}

// queryBool reports whether a query param is present and truthy. A bare flag
// (?ready) counts as true; explicit "false"/"0" turn it off.
func queryBool(q map[string][]string, key string) bool {
	v, ok := q[key]
	if !ok || len(v) == 0 {
		return false
	}
	switch strings.ToLower(v[0]) {
	case "", "1", "true", "yes":
		return true
	default:
		return false
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, api.ErrorResponse{Error: msg})
}
