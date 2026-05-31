package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alehatsman/moongit/internal/api"
	"github.com/alehatsman/moongit/internal/ci"
	"github.com/alehatsman/moongit/internal/storage"
)

// handleListCIRuns returns a repo's CI runs, newest-first. ?limit caps the page
// (default 100, max 1000 — enforced by storage.ListRuns).
func (s *Server) handleListCIRuns(w http.ResponseWriter, r *http.Request) {
	repoID, ok := s.lookupRepoOrFail(w, r)
	if !ok {
		return
	}

	limit := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = n
	}

	runs, err := storage.ListRuns(s.rdb, repoID, limit)
	if err != nil {
		s.logger.Error("ci list runs", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Optional ?kind=ci|agent filter so the Pipelines and Agents tabs each show
	// only their own runs.
	kind := storage.RunKind(r.URL.Query().Get("kind"))

	out := make([]api.CIRun, 0, len(runs))
	for _, run := range runs {
		if kind != "" && run.Kind != kind {
			continue
		}
		out = append(out, toAPIRun(run))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleGetCIRun returns a single run plus its jobs.
func (s *Server) handleGetCIRun(w http.ResponseWriter, r *http.Request) {
	repoID, ok := s.lookupRepoOrFail(w, r)
	if !ok {
		return
	}
	num, ok := runNumberOrFail(w, r)
	if !ok {
		return
	}

	run, err := storage.GetRun(s.rdb, repoID, num)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	if err != nil {
		s.logger.Error("ci get run", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	jobs, err := storage.ListJobs(s.rdb, run.ID)
	if err != nil {
		s.logger.Error("ci list jobs", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	detail := api.CIRunDetail{CIRun: toAPIRun(run), Jobs: make([]api.CIJob, len(jobs))}
	for i, j := range jobs {
		detail.Jobs[i] = toAPIJob(j)
	}
	// An agent run's conversation: the follow-up turns (issue body is turn 1).
	if run.Kind == storage.RunKindAgent {
		turns, err := storage.ListTurns(s.rdb, run.ID)
		if err != nil {
			s.logger.Error("ci list turns", "err", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		detail.Turns = make([]api.AgentTurn, len(turns))
		for i, t := range turns {
			detail.Turns[i] = toAPITurn(t)
		}
	}
	writeJSON(w, http.StatusOK, detail)
}

// handleRerunCIRun re-enqueues a fresh run for an existing run's commit/ref.
// It allocates a new run number (history is append-only — a rerun never
// mutates the original). Requires CI still enabled for the repo.
func (s *Server) handleRerunCIRun(w http.ResponseWriter, r *http.Request) {
	repoID, ok := s.lookupRepoOrFail(w, r)
	if !ok {
		return
	}
	num, ok := runNumberOrFail(w, r)
	if !ok {
		return
	}

	src, err := storage.GetRun(s.db, repoID, num)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	if err != nil {
		s.logger.Error("ci rerun get", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	enabled, err := storage.RepoCIEnabled(s.db, repoID)
	if err != nil {
		s.logger.Error("ci rerun enabled check", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !enabled {
		writeError(w, http.StatusConflict, "CI is disabled for this repo")
		return
	}

	run, err := storage.EnqueueRun(s.db, repoID, storage.NewRun{
		CommitSHA:    src.CommitSHA,
		CommitMsg:    src.CommitMsg,
		CommitAuthor: src.CommitAuthor,
		Ref:          src.Ref,
		Event:        src.Event,
		Trigger:      identityFromContext(r), // the agent that requested the rerun
	})
	if err != nil {
		s.logger.Error("ci rerun enqueue", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusAccepted, toAPIRun(run))
}

// handleTriggerCIRun starts a CI run for an arbitrary ref (branch, tag, or
// commit SHA) without a git push — the on-demand counterpart to the
// push-driven hook. It resolves the ref to a commit against the bare repo and
// enqueues a run with event "manual". Requires CI still enabled; the runner's
// mgitci.yml gate still applies at execution time, so triggering a commit that
// carries no pipeline simply yields a canceled run, exactly like a push.
func (s *Server) handleTriggerCIRun(w http.ResponseWriter, r *http.Request) {
	repoID, ok := s.lookupRepoOrFail(w, r)
	if !ok {
		return
	}

	var req api.TriggerCIRunRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Ref = strings.TrimSpace(req.Ref)
	if req.Ref == "" {
		writeError(w, http.StatusBadRequest, "ref is required")
		return
	}
	// A leading dash would let the ref masquerade as a git flag; reject it
	// rather than smuggle options into rev-parse.
	if strings.HasPrefix(req.Ref, "-") {
		writeError(w, http.StatusBadRequest, "invalid ref")
		return
	}

	enabled, err := storage.RepoCIEnabled(s.db, repoID)
	if err != nil {
		s.logger.Error("ci trigger enabled check", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !enabled {
		writeError(w, http.StatusConflict, "CI is disabled for this repo")
		return
	}

	owner := r.PathValue("owner")
	repo := strings.TrimSuffix(r.PathValue("repo"), ".git")
	bareRepo := filepath.Join(s.cfg.ReposDir, owner, repo+".git")

	// Resolve the ref to a concrete commit. ^{commit} peels annotated tags;
	// -q --verify turns an unknown ref into a clean non-zero exit instead of
	// echoing the input back.
	out, err := gitOutput(r.Context(), bareRepo, "rev-parse", "-q", "--verify", req.Ref+"^{commit}")
	sha := strings.TrimSpace(string(out))
	if err != nil || sha == "" {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("cannot resolve ref %q", req.Ref))
		return
	}

	msg, author := gitCommitMeta(bareRepo, sha)
	run, err := storage.EnqueueRun(s.db, repoID, storage.NewRun{
		CommitSHA:    sha,
		CommitMsg:    msg,
		CommitAuthor: author,
		Ref:          req.Ref,
		Event:        "manual",
		Trigger:      identityFromContext(r),
	})
	if err != nil {
		s.logger.Error("ci trigger enqueue", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.logger.Info("ci run triggered", "repo", owner+"/"+repo, "run", run.Number, "ref", req.Ref)
	writeJSON(w, http.StatusAccepted, toAPIRun(run))
}

// handleCIJobEvents streams a job's event log as Server-Sent Events: it replays
// the on-disk events.jsonl from the start, then live-tails while the run is
// still in flight, closing once the run reaches a terminal state and the file
// is drained. Clients resume after a drop via the Last-Event-ID header (or a
// ?last_event_id= query param); only events with a higher Seq are sent.
func (s *Server) handleCIJobEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	repoID, ok := s.lookupRepoOrFail(w, r)
	if !ok {
		return
	}
	num, ok := runNumberOrFail(w, r)
	if !ok {
		return
	}
	owner := r.PathValue("owner")
	repo := strings.TrimSuffix(r.PathValue("repo"), ".git")
	jobName := r.PathValue("job")

	run, err := storage.GetRun(s.rdb, repoID, num)
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	if err != nil {
		s.logger.Error("ci events get run", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Validate the job belongs to the run. This both 404s unknown jobs and
	// guarantees jobName is a real (already path-validated) name, so using it
	// to build the event-log path can't traverse out of the CI root.
	jobs, err := storage.ListJobs(s.rdb, run.ID)
	if err != nil {
		s.logger.Error("ci events list jobs", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !jobExists(jobs, jobName) {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}

	lastID := parseLastEventID(r)
	path := ci.EventLogPath(s.cfg.DataDir, owner, repo, num, jobName)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // don't let a proxy buffer the stream
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx := r.Context()
	const poll = 750 * time.Millisecond
	var offset int64

	for {
		events, nextOffset, rerr := ci.ReadEvents(path, offset)
		if rerr != nil {
			s.logger.Error("ci events read", "err", rerr)
			return
		}
		offset = nextOffset
		for _, ev := range events {
			if ev.Seq <= lastID {
				continue
			}
			if err := writeSSEEvent(w, ev); err != nil {
				return // client gone
			}
			lastID = ev.Seq
		}
		flusher.Flush()

		// Terminal check: once the run has finished, drain whatever is left and
		// stop. A skipped job writes no file at all, so the DB status — not the
		// stream — is the authoritative end signal.
		fresh, err := storage.GetRun(s.rdb, repoID, num)
		if err == nil && fresh.Status.Terminal() {
			// One final drain to catch anything flushed between the read above
			// and the status going terminal.
			tail, end, terr := ci.ReadEvents(path, offset)
			if terr == nil {
				offset = end
				for _, ev := range tail {
					if ev.Seq <= lastID {
						continue
					}
					if err := writeSSEEvent(w, ev); err != nil {
						return
					}
					lastID = ev.Seq
				}
				flusher.Flush()
			}
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(poll):
			// Heartbeat keeps intermediaries from idling the connection out and
			// surfaces a client disconnect promptly on the next write.
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// writeSSEEvent frames one event as an SSE message: the Seq is the event id
// (so the browser echoes it back as Last-Event-ID on reconnect), the event
// type is the stream's type, and the JSON-encoded event is the data payload.
func writeSSEEvent(w http.ResponseWriter, ev ci.Event) error {
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.Seq, ev.Type, data)
	return err
}

// parseLastEventID reads the resume point from the Last-Event-ID header (set
// automatically by EventSource on reconnect), falling back to a query param
// for clients that can't set the header. Unparseable or absent -> 0 (replay
// from the start).
func parseLastEventID(r *http.Request) int {
	v := r.Header.Get("Last-Event-ID")
	if v == "" {
		v = r.URL.Query().Get("last_event_id")
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func jobExists(jobs []storage.CIJob, name string) bool {
	for _, j := range jobs {
		if j.Name == name {
			return true
		}
	}
	return false
}

// runNumberOrFail parses the {number} path value as a positive run number.
func runNumberOrFail(w http.ResponseWriter, r *http.Request) (int, bool) {
	num, err := strconv.Atoi(r.PathValue("number"))
	if err != nil || num <= 0 {
		writeError(w, http.StatusBadRequest, "invalid run number")
		return 0, false
	}
	return num, true
}

func toAPIRun(run storage.CIRun) api.CIRun {
	return api.CIRun{
		Number:       run.Number,
		Kind:         string(run.Kind),
		IssueNumber:  run.IssueNumber,
		CommitSHA:    run.CommitSHA,
		CommitMsg:    run.CommitMsg,
		CommitAuthor: run.CommitAuthor,
		Ref:          run.Ref,
		Event:        run.Event,
		Trigger:      run.Trigger,
		Status:       string(run.Status),
		CreatedAt:    run.CreatedAt,
		StartedAt:    run.StartedAt,
		FinishedAt:   run.FinishedAt,
	}
}

func toAPIJob(j storage.CIJob) api.CIJob {
	return api.CIJob{
		Name:       j.Name,
		Needs:      j.Needs,
		Status:     string(j.Status),
		ExitCode:   j.ExitCode,
		StartedAt:  j.StartedAt,
		FinishedAt: j.FinishedAt,
	}
}
