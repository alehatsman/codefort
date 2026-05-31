package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/alehatsman/moongit/internal/api"
	"github.com/alehatsman/moongit/internal/storage"
)

// handleSpawnAgent starts an agent run for an issue: it enqueues a kind=agent
// run that checks out a base commit and works the issue in a container (the
// agent counterpart to handleTriggerCIRun). The base defaults to the repo's
// HEAD; an optional `ref` in the body pins a different branch/tag/commit. The
// run reuses the CI spine downstream, so its progress streams over the same
// run/job event endpoints. Credential plumbing and the Claude turn loop land in
// later units (#77, #76); this endpoint is the trigger half of #74.
func (s *Server) handleSpawnAgent(w http.ResponseWriter, r *http.Request) {
	repoID, ok := s.lookupRepoOrFail(w, r)
	if !ok {
		return
	}
	num, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || num <= 0 {
		writeError(w, http.StatusBadRequest, "invalid issue number")
		return
	}

	// The run must serve a real issue — 404 an unknown one before enqueuing.
	issue, err := storage.GetIssue(s.db, repoID, num)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "issue not found")
		return
	}
	if err != nil {
		s.logger.Error("agent spawn get issue", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Body is optional; an empty/absent body just means "default base".
	var req api.SpawnAgentRequest
	if r.Body != nil {
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
		if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	ref := strings.TrimSpace(req.Ref)
	if ref == "" {
		ref = "HEAD"
	}
	// A leading dash would let the ref masquerade as a git flag.
	if strings.HasPrefix(ref, "-") {
		writeError(w, http.StatusBadRequest, "invalid ref")
		return
	}

	owner := r.PathValue("owner")
	repo := strings.TrimSuffix(r.PathValue("repo"), ".git")
	bareRepo := filepath.Join(s.cfg.ReposDir, owner, repo+".git")

	// Resolve the base ref to a concrete commit so the agent checks out an
	// immutable tree (same peel/verify dance as the CI trigger).
	out, err := gitOutput(r.Context(), bareRepo, "rev-parse", "-q", "--verify", ref+"^{commit}")
	sha := strings.TrimSpace(string(out))
	if err != nil || sha == "" {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("cannot resolve ref %q", ref))
		return
	}

	msg, author := gitCommitMeta(bareRepo, sha)
	n := issue.Number
	run, err := storage.EnqueueRun(s.db, repoID, storage.NewRun{
		Kind:         storage.RunKindAgent,
		IssueNumber:  &n,
		CommitSHA:    sha,
		CommitMsg:    msg,
		CommitAuthor: author,
		Ref:          ref,
		Event:        "agent",
		Trigger:      identityFromContext(r),
	})
	if err != nil {
		s.logger.Error("agent spawn enqueue", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.logger.Info("agent run spawned", "repo", owner+"/"+repo, "issue", num, "run", run.Number, "ref", ref)
	writeJSON(w, http.StatusAccepted, toAPIRun(run))
}
