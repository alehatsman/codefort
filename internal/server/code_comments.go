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

// handleListCodeComments returns a repo's code comments on a branch. ?ref=
// selects the branch (default branch when absent), ?path= scopes to one file,
// and ?state= is one of open (default), resolved, or all. Each comment's
// Snippet is filled in from the referenced source lines so a reviewer — human
// or agent — sees the code without a second fetch.
func (s *Server) handleListCodeComments(w http.ResponseWriter, r *http.Request) {
	repoID, ok := s.lookupRepoOrFail(w, r)
	if !ok {
		return
	}
	repoDir, ok := s.repoDirOrFail(w, r)
	if !ok {
		return
	}
	ref, ok := s.resolveRef(w, r, repoDir)
	if !ok {
		return
	}
	path, err := cleanTreePath(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	state := r.URL.Query().Get("state")
	includeResolved := state == "all" || state == "resolved"

	comments, err := storage.ListCodeComments(s.rdb, repoID, ref, path, includeResolved)
	if err != nil {
		s.logger.Error("list code comments", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if state == "resolved" {
		open := comments[:0]
		for _, c := range comments {
			if c.Resolved {
				open = append(open, c)
			}
		}
		comments = open
	}

	s.fillSnippets(r, repoDir, ref, comments)
	writeJSON(w, http.StatusOK, comments)
}

// handleCreateCodeComment anchors a new comment to a file line range on a
// branch. The branch and path must exist; author and the ref's current HEAD
// are stamped server-side.
func (s *Server) handleCreateCodeComment(w http.ResponseWriter, r *http.Request) {
	repoID, ok := s.lookupRepoOrFail(w, r)
	if !ok {
		return
	}
	repoDir, ok := s.repoDirOrFail(w, r)
	if !ok {
		return
	}

	var req api.CreateCodeCommentRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	req.Body = strings.TrimSpace(req.Body)
	if req.Body == "" {
		writeError(w, http.StatusBadRequest, "body is required")
		return
	}

	path, err := cleanTreePath(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	req.Path = path

	if req.StartLine < 1 || req.EndLine < req.StartLine {
		writeError(w, http.StatusBadRequest, "invalid line range")
		return
	}

	// Default to the repo's HEAD branch; an explicit ref must be a real branch.
	req.Ref = strings.TrimSpace(req.Ref)
	if req.Ref == "" {
		req.Ref = headRef(r.Context(), repoDir)
	} else if !branchExists(r.Context(), repoDir, req.Ref) {
		writeError(w, http.StatusNotFound, "branch not found: "+req.Ref)
		return
	}

	// The anchored path must be a file on that branch.
	if typ, err := gitOutput(r.Context(), repoDir, "cat-file", "-t", req.Ref+":"+path); err != nil ||
		strings.TrimSpace(string(typ)) != "blob" {
		writeError(w, http.StatusNotFound, "path not found on branch: "+path)
		return
	}

	// Freeze the branch's current HEAD for drift context (best-effort).
	if sha, err := gitOutput(r.Context(), repoDir, "rev-parse", req.Ref); err == nil {
		req.CommitSha = strings.TrimSpace(string(sha))
	}

	req.Author = identityFromContext(r)

	c, err := storage.CreateCodeComment(s.db, repoID, req)
	if err != nil {
		s.logger.Error("create code comment", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

// handlePatchCodeComment toggles a comment's resolved flag. Author-only.
func (s *Server) handlePatchCodeComment(w http.ResponseWriter, r *http.Request) {
	id, ok := codeCommentIDOrFail(w, r)
	if !ok {
		return
	}
	var req api.UpdateCodeCommentRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.Resolved == nil {
		writeError(w, http.StatusBadRequest, "resolved is required")
		return
	}
	c, err := storage.SetCodeCommentResolved(s.db, id, *req.Resolved, identityFromContext(r))
	switch {
	case errors.Is(err, storage.ErrNotFound):
		writeError(w, http.StatusNotFound, "comment not found")
	case errors.Is(err, storage.ErrForbidden):
		writeError(w, http.StatusForbidden, "only the author can update this comment")
	case err != nil:
		s.logger.Error("patch code comment", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	default:
		writeJSON(w, http.StatusOK, c)
	}
}

// handleDeleteCodeComment removes a comment. Author-only.
func (s *Server) handleDeleteCodeComment(w http.ResponseWriter, r *http.Request) {
	id, ok := codeCommentIDOrFail(w, r)
	if !ok {
		return
	}
	err := storage.DeleteCodeComment(s.db, id, identityFromContext(r))
	switch {
	case errors.Is(err, storage.ErrNotFound):
		writeError(w, http.StatusNotFound, "comment not found")
	case errors.Is(err, storage.ErrForbidden):
		writeError(w, http.StatusForbidden, "only the author can delete this comment")
	case err != nil:
		s.logger.Error("delete code comment", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func codeCommentIDOrFail(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid comment id")
		return 0, false
	}
	return id, true
}

// fillSnippets populates each comment's Snippet with its referenced source
// lines, reading each distinct file at the ref just once. A missing file or
// out-of-range lines simply leave the snippet empty — comments outlive the
// code they point at.
func (s *Server) fillSnippets(r *http.Request, repoDir, ref string, comments []api.CodeComment) {
	cache := map[string][]string{}
	for i := range comments {
		c := &comments[i]
		lines, ok := cache[c.Path]
		if !ok {
			lines = fileLines(r, repoDir, ref, c.Path)
			cache[c.Path] = lines
		}
		c.Snippet = sliceLines(lines, c.StartLine, c.EndLine)
	}
}

// fileLines returns a file's content at ref split into lines, or nil on any
// error (binary, missing, oversized). Lines carry no trailing newline.
func fileLines(r *http.Request, repoDir, ref, path string) []string {
	content, err := gitOutput(r.Context(), repoDir, "cat-file", "blob", ref+":"+path)
	if err != nil || isBinary(content) || len(content) > maxBlobBytes {
		return nil
	}
	body := strings.TrimSuffix(string(content), "\n")
	return strings.Split(body, "\n")
}

// sliceLines returns lines[start..end] (1-based, inclusive) joined with \n,
// clamped to the available range. Empty when the range falls entirely outside.
func sliceLines(lines []string, start, end int) string {
	if len(lines) == 0 || start < 1 || start > len(lines) {
		return ""
	}
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[start-1:end], "\n")
}
