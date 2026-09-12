package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/alehatsman/codefort/internal/api"
	"github.com/alehatsman/codefort/internal/storage"
)

// handleListMembers returns all collaborators on a repo.
//
// GET /api/repos/{owner}/{repo}/members
func (s *Server) handleListMembers(w http.ResponseWriter, r *http.Request) {
	owner := r.PathValue("owner")
	repo := strings.TrimSuffix(r.PathValue("repo"), ".git")

	repoID, err := storage.LookupRepo(s.rdb, owner, repo)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "repo not registered: "+owner+"/"+repo)
		return
	}
	if err != nil {
		s.logger.Error("list members lookup", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	members, err := storage.ListRepoMembers(s.rdb, repoID)
	if err != nil {
		s.logger.Error("list members", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, members)
}

// handleAddMember grants a collaborator access to a repo. Only the repo owner
// may call this.
//
// POST /api/repos/{owner}/{repo}/members
func (s *Server) handleAddMember(w http.ResponseWriter, r *http.Request) {
	owner := r.PathValue("owner")
	repo := strings.TrimSuffix(r.PathValue("repo"), ".git")
	caller := identityFromContext(r)

	repoID, err := storage.LookupRepo(s.rdb, owner, repo)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "repo not registered: "+owner+"/"+repo)
		return
	}
	if err != nil {
		s.logger.Error("add member lookup", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	isOwner, err := storage.IsRepoOwner(s.rdb, repoID, caller)
	if err != nil {
		s.logger.Error("check owner", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !isOwner {
		writeError(w, http.StatusForbidden, "only the repo owner can manage members")
		return
	}

	var req api.AddMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Role == "" {
		req.Role = "write"
	}

	if err := storage.AddRepoMember(s.db, repoID, req.Username, req.Role); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user not found: "+req.Username)
			return
		}
		if errors.Is(err, storage.ErrInvalidInput) {
			writeError(w, http.StatusBadRequest, "role must be 'read' or 'write'")
			return
		}
		s.logger.Error("add member", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRemoveMember revokes a collaborator's access. Only the repo owner may
// call this.
//
// DELETE /api/repos/{owner}/{repo}/members/{username}
func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	owner := r.PathValue("owner")
	repo := strings.TrimSuffix(r.PathValue("repo"), ".git")
	target := r.PathValue("username")
	caller := identityFromContext(r)

	repoID, err := storage.LookupRepo(s.rdb, owner, repo)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "repo not registered: "+owner+"/"+repo)
		return
	}
	if err != nil {
		s.logger.Error("remove member lookup", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	isOwner, err := storage.IsRepoOwner(s.rdb, repoID, caller)
	if err != nil {
		s.logger.Error("check owner for remove", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !isOwner {
		writeError(w, http.StatusForbidden, "only the repo owner can manage members")
		return
	}

	if err := storage.RemoveRepoMember(s.db, repoID, target); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, target+" is not a member of this repo")
			return
		}
		s.logger.Error("remove member", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
