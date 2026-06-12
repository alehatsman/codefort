package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// handleCreateBranch creates a new branch from a base ref.
// POST /api/repos/{owner}/{repo}/branches
func (s *Server) handleCreateBranch(w http.ResponseWriter, r *http.Request) {
	repoDir, ok := s.repoDirOrFail(w, r)
	if !ok {
		return
	}

	var req struct {
		Name string `json:"name"`
		Base string `json:"base"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "branch name required")
		return
	}

	ctx := r.Context()

	// Resolve base ref — default to HEAD branch.
	base := req.Base
	if base == "" {
		base = headRef(ctx, repoDir)
	}

	sha, err := revParse(ctx, repoDir, base)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("base ref %q not found", base))
		return
	}

	if branchExists(ctx, repoDir, req.Name) {
		writeError(w, http.StatusConflict, fmt.Sprintf("branch %q already exists", req.Name))
		return
	}

	ref := "refs/heads/" + req.Name
	if err := updateRef(ctx, repoDir, ref, sha, zeroOID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create branch")
		return
	}

	writeJSON(w, http.StatusCreated, struct {
		Name string `json:"name"`
		SHA  string `json:"sha"`
	}{Name: req.Name, SHA: sha})
}
