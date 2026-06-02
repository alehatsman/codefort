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

// handleCreatePull opens a PR from head into base. Both must name existing
// local branches; author is stamped from the token. No diff is computed here —
// the detail endpoint embeds the compare.
func (s *Server) handleCreatePull(w http.ResponseWriter, r *http.Request) {
	repoID, ok := s.lookupRepoOrFail(w, r)
	if !ok {
		return
	}
	repoDir, ok := s.repoDirOrFail(w, r)
	if !ok {
		return
	}

	var req api.CreatePullRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	req.Base = strings.TrimSpace(req.Base)
	req.Head = strings.TrimSpace(req.Head)
	req.Title = strings.TrimSpace(req.Title)
	if req.Base == "" || req.Head == "" {
		writeError(w, http.StatusBadRequest, "base and head are required")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if req.Base == req.Head {
		writeError(w, http.StatusBadRequest, "base and head must differ")
		return
	}
	if !branchExists(r.Context(), repoDir, req.Base) {
		writeError(w, http.StatusNotFound, "branch not found: "+req.Base)
		return
	}
	if !branchExists(r.Context(), repoDir, req.Head) {
		writeError(w, http.StatusNotFound, "branch not found: "+req.Head)
		return
	}
	// Identity is stamped from the authenticated token; client-supplied author
	// is ignored.
	req.Author = identityFromContext(r)

	pr, err := storage.CreatePull(s.db, repoID, req)
	if err != nil {
		s.logger.Error("create pull", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, pr)
}

func (s *Server) handleListPulls(w http.ResponseWriter, r *http.Request) {
	repoID, ok := s.lookupRepoOrFail(w, r)
	if !ok {
		return
	}

	states, err := parsePRStates(r.URL.Query()["state"])
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))

	pulls, err := storage.ListPulls(s.rdb, repoID, states, query)
	if err != nil {
		s.logger.Error("list pulls", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, pulls)
}

// handleGetPull returns the PR plus the head-vs-base compare and the code
// review comments on its head branch. For a merged PR the compare is taken
// against the tips frozen at merge time (live refs would diff empty once head
// is folded into base); for an open PR it's the live base-vs-head. The compare
// is best-effort: if the needed commits/branches are gone it carries only
// base/head with an empty diff, so a stale PR still renders.
func (s *Server) handleGetPull(w http.ResponseWriter, r *http.Request) {
	num, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || num <= 0 {
		writeError(w, http.StatusBadRequest, "invalid pull request number")
		return
	}

	repoID, ok := s.lookupRepoOrFail(w, r)
	if !ok {
		return
	}
	repoDir, ok := s.repoDirOrFail(w, r)
	if !ok {
		return
	}

	pr, err := storage.GetPull(s.rdb, repoID, num)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "pull request not found")
		return
	}
	if err != nil {
		s.logger.Error("get pull", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	detail := api.PullRequestDetail{
		PullRequest: pr,
		Compare:     api.Compare{Base: pr.BaseRef, Head: pr.HeadRef, Commits: []api.Commit{}, Files: []api.DiffFile{}},
		Comments:    []api.CodeComment{},
	}
	// A merged PR's compare is computed from the tips frozen at merge time: once
	// head is merged into base, head is contained in base, so a live base..head
	// diff is empty. The frozen SHAs reproduce the exact pre-merge diff and
	// survive branch deletion. PRs merged before migration 16 have no frozen
	// SHAs and fall through to the live-ref path. The branch short names are
	// kept for display.
	if pr.State == api.PRMerged && pr.MergeBaseSHA != "" && pr.MergeHeadSHA != "" {
		if cmp, err := computeCompare(r.Context(), repoDir, pr.MergeBaseSHA, pr.MergeHeadSHA); err == nil {
			cmp.Base, cmp.Head = pr.BaseRef, pr.HeadRef
			detail.Compare = cmp
		}
	} else if branchExists(r.Context(), repoDir, pr.BaseRef) && branchExists(r.Context(), repoDir, pr.HeadRef) {
		// Open/stale PR: compare the live branches when both exist; otherwise
		// leave the empty shell so a PR whose branch was deleted still loads.
		if cmp, err := computeCompare(r.Context(), repoDir, pr.BaseRef, pr.HeadRef); err == nil {
			detail.Compare = cmp
		}
	}
	// Review threads reuse code_comments anchored to the head branch. Include
	// resolved ones so the full review history shows on the PR.
	if comments, err := storage.ListCodeComments(s.rdb, repoID, pr.HeadRef, "", true); err == nil {
		detail.Comments = comments
	}

	writeJSON(w, http.StatusOK, detail)
}

// handleUpdatePull applies a partial update of title/body/state. State may move
// to "closed" (abandon) or "open" (reopen); "merged" is rejected here — merging
// goes through the dedicated merge endpoint (#81) so the branches are actually
// joined, not just relabeled.
func (s *Server) handleUpdatePull(w http.ResponseWriter, r *http.Request) {
	num, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || num <= 0 {
		writeError(w, http.StatusBadRequest, "invalid pull request number")
		return
	}

	var req api.UpdatePullRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.State != nil {
		if !req.State.Valid() {
			writeError(w, http.StatusBadRequest, "invalid state: "+string(*req.State))
			return
		}
		if *req.State == api.PRMerged {
			writeError(w, http.StatusBadRequest, "cannot set state to merged; use the merge endpoint")
			return
		}
	}
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

	pr, err := storage.UpdatePull(s.db, repoID, num, req.Title, req.Body, req.State)
	if errors.Is(err, storage.ErrNoUpdateFields) {
		writeError(w, http.StatusBadRequest, "no fields to update (provide title, body, and/or state)")
		return
	}
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "pull request not found")
		return
	}
	if err != nil {
		s.logger.Error("update pull", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, pr)
}

// parsePRStates parses repeated/comma-separated ?state= values into a validated
// slice. Empty input means "any" (nil slice).
func parsePRStates(raw []string) ([]api.PRState, error) {
	var states []api.PRState
	for _, v := range raw {
		for part := range strings.SplitSeq(v, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			st := api.PRState(part)
			if !st.Valid() {
				return nil, errors.New("invalid state: " + part)
			}
			states = append(states, st)
		}
	}
	return states, nil
}
