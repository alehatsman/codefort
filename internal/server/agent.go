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

	// Resolve the execution model: an explicit request value wins (must be
	// known), else the operator's configured default, else the built-in
	// default (#110).
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = storage.SettingValue(s.db, storage.SettingAgentExecutionModel)
	}
	if model == "" {
		model = storage.DefaultExecutionModel
	}
	if !storage.ValidExecutionModel(model) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid execution model %q", model))
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
		Kind:            storage.RunKindAgent,
		IssueNumber:     &n,
		ExecutionModel:  model,
		PilotAllowShell: req.AllowShell,
		CommitSHA:       sha,
		CommitMsg:       msg,
		CommitAuthor:    author,
		Ref:             ref,
		Event:           "agent",
		Trigger:         identityFromContext(r),
	})
	if err != nil {
		s.logger.Error("agent spawn enqueue", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.logger.Info("agent run spawned", "repo", owner+"/"+repo, "issue", num, "run", run.Number, "ref", ref)
	writeJSON(w, http.StatusAccepted, toAPIRun(run))
}

// handleCreateAgentTurn queues a follow-up turn (a human message) on an agent
// run. The run must be an agent run that hasn't finished; a turn sent while a
// turn is in flight simply queues behind it. The turn-dispatch loop picks it up,
// resumes the claude session, and streams the response onto the run's event log.
func (s *Server) handleCreateAgentTurn(w http.ResponseWriter, r *http.Request) {
	repoID, ok := s.lookupRepoOrFail(w, r)
	if !ok {
		return
	}
	num, ok := runNumberOrFail(w, r)
	if !ok {
		return
	}

	var req api.CreateAgentTurnRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}

	run, err := storage.GetRun(s.db, repoID, num)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	if err != nil {
		s.logger.Error("agent turn get run", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if run.Kind != storage.RunKindAgent {
		writeError(w, http.StatusBadRequest, "not an agent run")
		return
	}
	if run.Status.Terminal() {
		writeError(w, http.StatusConflict, "agent run has finished")
		return
	}

	turn, err := storage.EnqueueTurn(s.db, run.ID, identityFromContext(r), req.Text)
	if err != nil {
		s.logger.Error("agent turn enqueue", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusAccepted, toAPITurn(turn))
}

// handleFinishAgentRun accepts a parked agent run: it transitions the run to
// finishing, after which the runner materializes the agent/issue-<n> branch and
// posts the summary comment (#78). The run must be an agent run currently
// awaiting input — finishing a run that's mid-turn or already terminal is a 409.
func (s *Server) handleFinishAgentRun(w http.ResponseWriter, r *http.Request) {
	repoID, ok := s.lookupRepoOrFail(w, r)
	if !ok {
		return
	}
	num, ok := runNumberOrFail(w, r)
	if !ok {
		return
	}

	run, err := storage.GetRun(s.db, repoID, num)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	if err != nil {
		s.logger.Error("agent finish get run", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if run.Kind != storage.RunKindAgent {
		writeError(w, http.StatusBadRequest, "not an agent run")
		return
	}

	// CAS awaiting_input -> finishing; ErrNotFound means it wasn't parked.
	if err := storage.MarkRunFinishing(s.db, run.ID); errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusConflict, "run is not awaiting input (busy or already finished)")
		return
	} else if err != nil {
		s.logger.Error("agent finish mark", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	run.Status = storage.RunFinishing
	writeJSON(w, http.StatusAccepted, toAPIRun(run))
}

// handleCancelAgentRun force-stops a running agent run (#146). Unlike finish —
// which only accepts a parked (awaiting_input) run and keeps the work — cancel
// is valid from any non-terminal state (queued/running/awaiting_input/
// finishing), interrupts an in-flight turn, and discards the workspace. A run
// that's already terminal is a 409; a non-agent run is a 400.
func (s *Server) handleCancelAgentRun(w http.ResponseWriter, r *http.Request) {
	repoID, ok := s.lookupRepoOrFail(w, r)
	if !ok {
		return
	}
	num, ok := runNumberOrFail(w, r)
	if !ok {
		return
	}

	run, err := storage.GetRun(s.db, repoID, num)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	if err != nil {
		s.logger.Error("agent cancel get run", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if run.Kind != storage.RunKindAgent {
		writeError(w, http.StatusBadRequest, "not an agent run")
		return
	}
	if s.agentCanceler == nil {
		writeError(w, http.StatusServiceUnavailable, "cancel unavailable (no runner)")
		return
	}
	if !s.agentCanceler.CancelAgentRun(run.ID) {
		writeError(w, http.StatusConflict, "run is already finished")
		return
	}

	run.Status = storage.RunCanceled
	writeJSON(w, http.StatusAccepted, toAPIRun(run))
}

func toAPITurn(t storage.AgentTurn) api.AgentTurn {
	return api.AgentTurn{
		Seq:        t.Seq,
		Author:     t.Author,
		Body:       t.Body,
		Status:     string(t.Status),
		CreatedAt:  t.CreatedAt,
		FinishedAt: t.FinishedAt,
	}
}
