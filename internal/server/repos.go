package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/alehatsman/moongit/internal/api"
	"github.com/alehatsman/moongit/internal/ci"
	"github.com/alehatsman/moongit/internal/storage"
)

func (s *Server) handleListRepos(w http.ResponseWriter, r *http.Request) {
	caller := identityFromContext(r)
	rows, err := storage.ListReposForCaller(s.rdb, caller)
	if err != nil {
		s.logger.Error("list repos", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := make([]api.Repo, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAPIRepo(row))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetRepo(w http.ResponseWriter, r *http.Request) {
	owner := r.PathValue("owner")
	repo := strings.TrimSuffix(r.PathValue("repo"), ".git")

	row, err := storage.GetRepoSummary(s.rdb, owner, repo)
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

func (s *Server) handleCreateRepo(w http.ResponseWriter, r *http.Request) {
	var req api.CreateRepoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	owner := strings.TrimSpace(req.Owner)
	name := strings.TrimSuffix(strings.TrimSpace(req.Name), ".git")
	if !validRepoComponent(owner) {
		writeError(w, http.StatusBadRequest, "invalid owner (allowed: letters, digits, . _ -)")
		return
	}
	if !validRepoComponent(name) {
		writeError(w, http.StatusBadRequest, "invalid repo name (allowed: letters, digits, . _ -)")
		return
	}

	// Reject up front so the UI can surface a clean 409 instead of
	// silently reusing an existing repo (CreateRepo is idempotent).
	if _, err := storage.LookupRepo(s.rdb, owner, name); err == nil {
		writeError(w, http.StatusConflict, "repo already exists: "+owner+"/"+name)
		return
	} else if !errors.Is(err, storage.ErrNotFound) {
		s.logger.Error("lookup repo", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	repoID, _, err := CreateRepo(s.db, s.cfg.ReposDir, owner, name)
	if err != nil {
		s.logger.Error("create repo", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if vis := strings.TrimSpace(req.Visibility); vis == "private" {
		if err := storage.SetRepoVisibility(s.db, repoID, "private"); err != nil {
			s.logger.Error("set repo visibility", "err", err)
			// Non-fatal — repo is created; just log and proceed.
		}
	}

	row, err := storage.GetRepoSummary(s.db, owner, name)
	if err != nil {
		s.logger.Error("get repo after create", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, toAPIRepo(row))
}

// repoComponentRe constrains owner/name to a path-safe charset so they map
// cleanly onto the on-disk repos dir and the git smart-HTTP routes.
var repoComponentRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func validRepoComponent(s string) bool {
	if s == "" || s == "." || s == ".." || len(s) > 100 {
		return false
	}
	return repoComponentRe.MatchString(s)
}

// CreateRepo provisions a bare git repository on disk and registers it in the
// database. Idempotent: an existing on-disk repo or DB row is reused. Returns
// the repo id and its on-disk path. Shared by the HTTP API and the CLI.
func CreateRepo(db *sql.DB, reposDir, owner, name string) (int64, string, error) {
	repoDir := filepath.Join(reposDir, owner, name+".git")
	if _, err := os.Stat(filepath.Join(repoDir, "HEAD")); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(repoDir, 0o755); err != nil {
			return 0, "", fmt.Errorf("mkdir repo: %w", err)
		}
		// -b main pins the bare repo's HEAD to refs/heads/main so the
		// default-branch doesn't follow the user's local git config (which
		// is commonly still `master` on older boxes). Without this, the
		// first push of a `main` branch leaves HEAD pointing at an unborn
		// `master` and read endpoints (tree/blob) show an empty repo even
		// though objects are present.
		out, err := exec.Command("git", "init", "--bare", "-b", "main", repoDir).CombinedOutput()
		if err != nil {
			return 0, "", fmt.Errorf("git init --bare: %w: %s", err, strings.TrimSpace(string(out)))
		}
	}

	// Install (or refresh) the CI post-receive hook on every call so existing
	// repos pick it up too. Cheap and idempotent.
	if err := WritePostReceiveHook(repoDir); err != nil {
		return 0, "", fmt.Errorf("write post-receive hook: %w", err)
	}

	id, err := storage.EnsureRepo(db, owner, name)
	if err != nil {
		return 0, "", fmt.Errorf("ensure repo: %w", err)
	}
	return id, repoDir, nil
}

func toAPIRepo(r storage.RepoSummary) api.Repo {
	vis := r.Visibility
	if vis == "" {
		vis = "public"
	}
	return api.Repo{
		ID:              r.ID,
		Owner:           r.Owner,
		Name:            r.Name,
		CreatedAt:       time.Unix(r.CreatedAt, 0).UTC(),
		OpenIssues:      r.OpenIssues,
		TotalIssues:     r.TotalIssues,
		CIEnabled:       r.CIEnabled,
		RequireApproval: r.RequireApproval,
		CIStatus:        r.CIStatus,
		CINumber:        r.CINumber,
		OpenPulls:       r.OpenPulls,
		OpenReviews:     r.OpenReviews,
		ActiveAgents:    r.ActiveAgents,
		Visibility:      vis,
	}
}

// handleUpdateRepo applies a partial update to a repo's settings.
// Mutable fields: ci_enabled, visibility, require_approval. Returns the updated
// repo summary.
func (s *Server) handleUpdateRepo(w http.ResponseWriter, r *http.Request) {
	owner := r.PathValue("owner")
	repo := strings.TrimSuffix(r.PathValue("repo"), ".git")

	var req api.UpdateRepoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if req.CIEnabled == nil && req.Visibility == nil && req.RequireApproval == nil {
		writeError(w, http.StatusBadRequest, "no fields to update (provide ci_enabled, visibility, or require_approval)")
		return
	}

	repoID, err := storage.LookupRepo(s.db, owner, repo)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "repo not registered: "+owner+"/"+repo)
		return
	}
	if err != nil {
		s.logger.Error("update repo lookup", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if req.CIEnabled != nil {
		if err := storage.SetRepoCIEnabled(s.db, repoID, *req.CIEnabled); err != nil {
			s.logger.Error("update repo ci_enabled", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	if req.RequireApproval != nil {
		if err := storage.SetRepoRequireApproval(s.db, repoID, *req.RequireApproval); err != nil {
			s.logger.Error("update repo require_approval", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	if req.Visibility != nil {
		if err := storage.SetRepoVisibility(s.db, repoID, *req.Visibility); err != nil {
			if errors.Is(err, storage.ErrInvalidInput) {
				writeError(w, http.StatusBadRequest, "visibility must be 'public' or 'private'")
				return
			}
			s.logger.Error("update repo visibility", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}

	row, err := storage.GetRepoSummary(s.db, owner, repo)
	if err != nil {
		s.logger.Error("get repo after update", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, toAPIRepo(row))
}

// handleDeleteRepo removes a repo entirely: its database row (cascading to all
// issues, runs, comments, pulls, and events) and its on-disk bare git dir.
// Destructive and irreversible. The DB row goes first so a half-failure leaves
// no dangling registration pointing at a missing git dir; the git dir is then
// removed best-effort. Returns 204 on success, 404 if the repo isn't
// registered.
//
// Auth posture matches the rest of the data plane (create/update/delete issue):
// any valid token may delete. The /api withAuth middleware already gates this
// on a valid token.
func (s *Server) handleDeleteRepo(w http.ResponseWriter, r *http.Request) {
	owner := r.PathValue("owner")
	repo := strings.TrimSuffix(r.PathValue("repo"), ".git")

	repoID, err := storage.LookupRepo(s.db, owner, repo)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "repo not registered: "+owner+"/"+repo)
		return
	}
	if err != nil {
		s.logger.Error("delete repo lookup", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Refuse while runs are in flight. The cascade would delete the ci_runs
	// rows out from under a live runner goroutine (whose status writes then
	// no-op, leaving a container/workspace running until the next restart's
	// sweep). 409 with a count so the caller cancels or waits, then retries.
	if active, err := storage.CountActiveRuns(s.db, repoID); err != nil {
		s.logger.Error("delete repo active runs", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	} else if active > 0 {
		writeError(w, http.StatusConflict, fmt.Sprintf(
			"repo %s/%s has %d in-flight run(s); cancel or wait for them to finish before deleting",
			owner, repo, active))
		return
	}

	if err := storage.DeleteRepo(s.db, repoID); err != nil {
		s.logger.Error("delete repo", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Remove the bare git dir, then prune the now-orphaned owner namespace dir
	// if it's empty. Both are best-effort: the DB row (the source of truth for
	// what's registered) is already gone, so a leftover dir is cosmetic and
	// must not turn a successful delete into a 500.
	repoDir := filepath.Join(s.cfg.ReposDir, owner, repo+".git")
	if err := os.RemoveAll(repoDir); err != nil {
		s.logger.Error("delete repo git dir", "dir", repoDir, "err", err)
	} else {
		// os.Remove only succeeds on an empty dir, which is exactly the prune
		// we want; a non-empty owner dir (other repos) errors and is left be.
		_ = os.Remove(filepath.Join(s.cfg.ReposDir, owner))
	}

	// Remove the repo's CI/agent event-log tree under the data dir too — it
	// lives outside ReposDir (see ci.RepoLogDir), so the git-dir removal above
	// doesn't touch it, and without this every run's logs would outlive the
	// repo. Best-effort and same owner-dir prune as above.
	logDir := ci.RepoLogDir(s.cfg.DataDir, owner, repo)
	if err := os.RemoveAll(logDir); err != nil {
		s.logger.Error("delete repo log dir", "dir", logDir, "err", err)
	} else {
		_ = os.Remove(filepath.Join(s.cfg.DataDir, "ci", owner))
	}

	// Emit the event without a repo_id: the repos row is gone and events
	// reference repos(id) ON DELETE CASCADE, so a repo-scoped event would be
	// cascade-deleted along with it. owner/name in the payload identify it.
	s.emit("repo.deleted", 0, identityFromContext(r), map[string]any{
		"owner": owner,
		"name":  repo,
	})

	w.WriteHeader(http.StatusNoContent)
}
