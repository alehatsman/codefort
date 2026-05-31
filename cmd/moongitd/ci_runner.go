package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alehatsman/moongit/internal/ci"
	"github.com/alehatsman/moongit/internal/config"
	"github.com/alehatsman/moongit/internal/storage"
)

// stepResult is the subset of `mooncake step` JSON the runner maps onto the
// event stream.
type stepResult struct {
	RC         int    `json:"rc"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	DurationMS int    `json:"duration_ms"`
	Changed    bool   `json:"changed"`
	Failed     bool   `json:"failed"`
	Skipped    bool   `json:"skipped"`
	Action     string `json:"action"`
	Error      string `json:"error"`
}

// The runner's external boundaries, injected so the orchestration is testable
// without the real git / mooncake / docker binaries.
type (
	// stepExecutor runs one mooncake step (YAML) in workDir. It backs the host
	// session and is the unit tests' injection point.
	stepExecutor func(ctx context.Context, workDir, stepYAML string) (stepResult, error)
	// checkoutFunc materializes the repo tree at commitSHA into workDir.
	checkoutFunc func(ctx context.Context, bareRepo, commitSHA, workDir string) error
	// pipelineReader reads mgitci.yml at commitSHA; ok=false means absent.
	pipelineReader func(bareRepo, commitSHA string) (raw []byte, ok bool, err error)
)

// jobSession executes one job's steps in some environment and is closed when
// the job finishes. It is the runner's isolation seam: runJob emits the same
// event stream regardless of whether the session runs steps on the host or in
// a per-job container.
type jobSession interface {
	Exec(ctx context.Context, stepYAML string) (stepResult, error)
	Close() error
}

// sessionFactory opens a jobSession for one job. name is a stable
// docker-safe container name; workDir is the checked-out (bind-mountable)
// workspace; image is the resolved container image (ignored by the host
// session).
type sessionFactory func(ctx context.Context, name, workDir, image string) (jobSession, error)

// ciRunner executes queued CI runs in-process, emitting the mooncake-shaped
// event stream per job. Everything downstream (storage status, API, UI)
// consumes that stream, so an agentd-backed runner can later replace this one
// behind the same boundary (#26).
type ciRunner struct {
	db     *sql.DB
	cfg    *config.Config
	logger *slog.Logger

	newSession   sessionFactory
	checkout     checkoutFunc
	readPipeline pipelineReader
}

func newCIRunner(db *sql.DB, cfg *config.Config, logger *slog.Logger) *ciRunner {
	r := &ciRunner{
		db:           db,
		cfg:          cfg,
		logger:       logger,
		checkout:     gitCheckout,
		readPipeline: gitReadPipeline,
	}
	if cfg.CIIsolation == "none" {
		// Legacy path: steps run on the host as the moongitd user.
		r.newSession = func(_ context.Context, _, workDir, _ string) (jobSession, error) {
			return &hostSession{workDir: workDir, exec: runMooncakeStep}, nil
		}
	} else {
		// Default: one throwaway container per job, steps run via docker exec.
		r.newSession = func(ctx context.Context, name, workDir, image string) (jobSession, error) {
			return openDockerSession(ctx, logger, name, workDir, image)
		}
	}
	return r
}

// runCIRunner launches the in-process CI runner beside the reapers. It claims
// and dispatches up to CIRunConcurrency runs at a time (default 1). Runs until
// ctx is cancelled.
func runCIRunner(ctx context.Context, db *sql.DB, cfg *config.Config, logger *slog.Logger) {
	newCIRunner(db, cfg, logger).run(ctx)
}

func (r *ciRunner) run(ctx context.Context) {
	interval := r.cfg.CIPollInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	runConc := max(r.cfg.CIRunConcurrency, 1)
	agentConc := max(r.cfg.AgentRunConcurrency, 1)
	r.logger.Info("ci runner started", "poll", interval, "run_timeout", r.cfg.CIRunTimeout, "isolation", r.cfg.CIIsolation, "run_concurrency", runConc, "agent_concurrency", agentConc)

	// A restart can strand runs mid-flight: their status writes never committed,
	// so they sit 'running' with no goroutine driving them. Nothing can be
	// legitimately in flight before we take our first run, so finalize any such
	// orphans now rather than leaving the UI with a job stuck forever.
	if n, err := storage.ReconcileOrphanRuns(r.db); err != nil {
		r.logger.Error("ci reconcile orphan runs", "err", err)
	} else if n > 0 {
		r.logger.Info("ci reconciled orphaned runs", "count", n)
	}

	// A crashed runner can leave job containers behind; reap them before
	// taking new work so they don't accumulate.
	if r.cfg.CIIsolation == "docker" {
		sweepOrphanContainers(ctx, r.logger)
	}

	// CI and agent runs drain from independent pools so a burst of one kind
	// never starves the other. wg tracks in-flight runs across both pools so
	// shutdown drains them: run() returns only once every dispatched
	// executeRun has finalized its run against the still-open DB (the
	// restart-drain contract — see cmd/moongitd/main.go).
	ciSem := make(chan struct{}, runConc)
	agentSem := make(chan struct{}, agentConc)
	var wg sync.WaitGroup
	defer wg.Wait()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		// Dispatch every claimable run of each kind before sleeping.
		r.drainKind(ctx, &wg, ciSem, storage.RunKindCI, r.runLease())
		r.drainKind(ctx, &wg, agentSem, storage.RunKindAgent, r.agentLease())
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// drainKind claims and dispatches every currently-claimable run of one kind,
// bounded by its pool's slots, returning when nothing more is claimable or the
// context is cancelled. A slot is taken *before* claiming so a claimed run is
// never held without a slot to execute it (its lease would tick while it
// waited). Each dispatched run is tracked on wg for the shutdown drain.
func (r *ciRunner) drainKind(ctx context.Context, wg *sync.WaitGroup, sem chan struct{}, kind storage.RunKind, lease time.Duration) {
	for {
		// Once shutdown starts, stop claiming new work; in-flight runs drain
		// via the caller's wg.Wait. Guarding here also keeps us from claiming a
		// queued run only to immediately error it.
		if ctx.Err() != nil {
			return
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return
		}
		run, err := storage.ClaimNextRunOfKind(r.db, kind, lease)
		if err != nil {
			<-sem // release the unused slot
			if !errors.Is(err, storage.ErrNoRunQueued) {
				r.logger.Error("ci claim", "kind", kind, "err", err)
			}
			return
		}
		wg.Add(1)
		go func(run storage.CIRun) {
			defer wg.Done()
			defer func() { <-sem }()
			r.executeRun(ctx, run)
		}(run)
	}
}

// runLease is the claim lease for runs: long enough that a legitimately
// running run (bounded by CIRunTimeout) is never stolen, but a crashed
// runner's 'running' row eventually expires and re-runs. No timeout means no
// steal (a run holds until done).
func (r *ciRunner) runLease() time.Duration {
	if r.cfg.CIRunTimeout <= 0 {
		return 0
	}
	return r.cfg.CIRunTimeout + 5*time.Minute
}

// agentLease is the claim lease for agent runs, derived from AgentRunTimeout
// the same way runLease derives from CIRunTimeout: long enough that a live run
// is never stolen, short enough that a crashed runner's 'running' agent row
// eventually expires and re-runs.
func (r *ciRunner) agentLease() time.Duration {
	if r.cfg.AgentRunTimeout <= 0 {
		return 0
	}
	return r.cfg.AgentRunTimeout + 5*time.Minute
}

// executeRun runs one claimed run end-to-end: gate, checkout, per-job step
// execution emitting the event stream, status mirroring, and workspace
// cleanup.
func (r *ciRunner) executeRun(parent context.Context, run storage.CIRun) {
	// An agent run reuses this same claim/lease/drain spine but executes a
	// containerized Claude session against an issue instead of a translated
	// mgitci.yml. Branch here so everything upstream (claiming, concurrency,
	// reconcile, retention) stays shared.
	if run.Kind == storage.RunKindAgent {
		r.executeAgentRun(parent, run)
		return
	}

	log := r.logger.With("run_id", run.ID, "run", run.Number, "sha", short(run.CommitSHA))

	owner, name, err := storage.RepoIdent(r.db, run.RepoID)
	if err != nil {
		log.Error("ci resolve repo", "err", err)
		r.finish(run.ID, storage.RunError)
		return
	}
	log = log.With("repo", owner+"/"+name)

	// Gate: repo must be CI-enabled AND mgitci.yml must exist at the commit.
	enabled, err := storage.RepoCIEnabled(r.db, run.RepoID)
	if err != nil {
		log.Error("ci enabled check", "err", err)
		r.finish(run.ID, storage.RunError)
		return
	}
	bareRepo := filepath.Join(r.cfg.ReposDir, owner, name+".git")
	raw, ok, err := r.readPipeline(bareRepo, run.CommitSHA)
	if err != nil {
		log.Error("ci read mgitci.yml", "err", err)
		r.finish(run.ID, storage.RunError)
		return
	}
	if !enabled || !ok {
		log.Info("ci run skipped (gated)", "enabled", enabled, "has_pipeline", ok)
		r.finish(run.ID, storage.RunCanceled)
		return
	}

	pipeline, err := ci.Parse(raw)
	if err != nil {
		log.Error("ci parse mgitci.yml", "err", err)
		r.finish(run.ID, storage.RunError)
		return
	}
	order, err := pipeline.TopoOrder()
	if err != nil {
		log.Error("ci topo order", "err", err)
		r.finish(run.ID, storage.RunError)
		return
	}

	// Fresh workspace; always cleaned up.
	workDir := filepath.Join(r.cfg.DataDir, "ci", "work", strconv.FormatInt(run.ID, 10))
	if err := os.RemoveAll(workDir); err == nil {
		err = os.MkdirAll(workDir, 0o755)
	}
	if err != nil {
		log.Error("ci workspace", "err", err)
		r.finish(run.ID, storage.RunError)
		return
	}
	defer os.RemoveAll(workDir)

	ctx := parent
	if r.cfg.CIRunTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(parent, r.cfg.CIRunTimeout)
		defer cancel()
	}

	if err := r.checkout(ctx, bareRepo, run.CommitSHA, workDir); err != nil {
		log.Error("ci checkout", "err", err)
		r.finish(run.ID, storage.RunError)
		return
	}

	// Create job rows up front, in topo order.
	jobIDs := make(map[string]int64, len(order))
	for _, jn := range order {
		job, err := storage.CreateJob(r.db, run.ID, jn, pipeline.Jobs[jn].Needs)
		if err != nil {
			log.Error("ci create job", "job", jn, "err", err)
			r.finish(run.ID, storage.RunError)
			return
		}
		jobIDs[jn] = job.ID
	}

	// Schedule jobs in dependency waves: every job whose needs have all
	// succeeded runs concurrently (bounded by CIJobConcurrency); a job whose
	// dependency failed/was skipped is itself skipped, cascading. So the DAG's
	// available parallelism is used — independent roots run together — while the
	// `needs` ordering is still honored.
	limit := max(r.cfg.CIJobConcurrency, 1)
	status := make(map[string]storage.JobStatus, len(order))
	pending := make(map[string]bool, len(order))
	for _, jn := range order {
		pending[jn] = true
	}
	anyNotSuccess := false

	for len(pending) > 0 && ctx.Err() == nil {
		var ready []string
		for jn := range pending {
			isReady, isSkip := classifyJob(pipeline.Jobs[jn].Needs, status)
			switch {
			case isSkip:
				log.Info("ci job skipped (dependency not satisfied)", "job", jn)
				r.finishJob(jobIDs[jn], storage.JobSkipped, nil)
				status[jn] = storage.JobSkipped
				delete(pending, jn)
				anyNotSuccess = true
			case isReady:
				ready = append(ready, jn)
			}
		}
		if len(ready) == 0 {
			// Nothing ready this pass. Either we just skipped a cascade (loop
			// again to propagate) or all remaining jobs await an in-flight wave;
			// since a wave is fully awaited below, "no ready, no skip" can only
			// mean a malformed DAG — break rather than spin.
			if len(pending) == 0 {
				break
			}
			break
		}
		sort.Strings(ready) // deterministic launch order (logs/tests)
		for jn, st := range r.runWave(ctx, owner, name, run.Number, jobIDs, pipeline, ready, workDir, limit) {
			status[jn] = st
			delete(pending, jn)
			if st != storage.JobSuccess {
				anyNotSuccess = true
			}
		}
	}
	// A run-timeout / shutdown can leave jobs pending; the run is errored below.

	final := storage.RunSuccess
	switch {
	case ctx.Err() != nil:
		final = storage.RunError
	case anyNotSuccess:
		final = storage.RunFailed
	}
	r.finish(run.ID, final)
	log.Info("ci run finished", "status", final)
}

// runJob executes one job's steps via mooncake, emitting the event stream into
// the job's events.jsonl, and returns its terminal status.
func (r *ciRunner) runJob(ctx context.Context, owner, repo string, runNum int, jobName string, jobID int64, job ci.Job, workDir string) storage.JobStatus {
	log := r.logger.With("run", runNum, "job", jobName)

	steps, err := ci.MooncakeSteps(job)
	if err != nil {
		log.Error("ci translate steps", "err", err)
		r.finishJob(jobID, storage.JobError, nil)
		return storage.JobError
	}

	elog, err := ci.OpenEventLog(r.cfg.DataDir, owner, repo, runNum, jobName)
	if err != nil {
		log.Error("ci open event log", "err", err)
		r.finishJob(jobID, storage.JobError, nil)
		return storage.JobError
	}
	defer elog.Close()

	if err := storage.StartJob(r.db, jobID); err != nil {
		log.Error("ci start job", "err", err)
	}
	r.emit(elog, ci.EventRunStarted, map[string]any{"total_steps": len(steps)})
	r.emit(elog, ci.EventPlanLoaded, map[string]any{"total_steps": len(steps)})

	// Open the job's execution environment (a per-job container under docker
	// isolation, or the host otherwise). A failure here — e.g. the image is
	// missing or docker is down — fails the job loudly rather than silently
	// falling back to the host.
	image := job.Image
	if image == "" {
		image = r.cfg.CIDefaultImage
	}
	sess, err := r.newSession(ctx, containerName(jobID, jobName), workDir, image)
	if err != nil {
		r.emit(elog, ci.EventStepStderr, map[string]any{
			"step_id": "session", "stream": "stderr", "line": err.Error(), "line_number": 1,
		})
		r.emit(elog, ci.EventRunFailed, map[string]any{"error": err.Error()})
		r.finishJob(jobID, storage.JobError, nil)
		log.Error("ci open session", "image", image, "err", err)
		return storage.JobError
	}
	defer sess.Close()

	for i, step := range steps {
		stepID := fmt.Sprintf("step-%04d", i+1)
		r.emit(elog, ci.EventStepStarted, map[string]any{
			"step_id": stepID, "action": step.Action, "name": step.Label, "global_step": i + 1,
		})

		res, execErr := sess.Exec(ctx, step.YAML)
		if execErr != nil {
			// Couldn't run the step (mooncake missing, or ctx timeout/cancel).
			r.emit(elog, ci.EventStepStderr, map[string]any{
				"step_id": stepID, "stream": "stderr", "line": execErr.Error(), "line_number": 1,
			})
			r.emit(elog, ci.EventStepCompleted, map[string]any{
				"step_id": stepID, "result": map[string]any{"rc": -1, "failed": true, "status": "error"},
			})
			r.emit(elog, ci.EventRunFailed, map[string]any{"step_id": stepID, "error": execErr.Error()})
			r.finishJob(jobID, storage.JobError, nil)
			log.Error("ci step exec", "step", stepID, "err", execErr)
			return storage.JobError
		}

		emitLines(r, elog, stepID, ci.EventStepStdout, "stdout", res.Stdout)
		emitLines(r, elog, stepID, ci.EventStepStderr, "stderr", res.Stderr)

		stepStatus := "ok"
		failed := res.Failed || res.RC != 0
		switch {
		case failed:
			stepStatus = "failed"
		case res.Skipped:
			stepStatus = "skipped"
		}
		r.emit(elog, ci.EventStepCompleted, map[string]any{
			"step_id": stepID, "duration_ms": res.DurationMS, "changed": res.Changed,
			"result": map[string]any{
				"rc": res.RC, "failed": res.Failed, "status": stepStatus,
				"stdout": res.Stdout, "stderr": res.Stderr,
			},
		})

		if failed {
			r.emit(elog, ci.EventRunFailed, map[string]any{"step_id": stepID, "rc": res.RC})
			rc := res.RC
			r.finishJob(jobID, storage.JobFailed, &rc)
			log.Info("ci job failed", "step", stepID, "rc", rc)
			return storage.JobFailed
		}
	}

	r.emit(elog, ci.EventRunCompleted, map[string]any{"total_steps": len(steps)})
	zero := 0
	r.finishJob(jobID, storage.JobSuccess, &zero)
	return storage.JobSuccess
}

// runWave runs jobs concurrently — each in its own goroutine, at most `limit`
// at once — and returns their terminal statuses keyed by job name. runJob is
// self-contained (its own per-job container, event log, and short DB txns), so
// the only shared state here is the writer pool, which serializes the brief
// status writes; nothing in this function mutates shared maps.
func (r *ciRunner) runWave(ctx context.Context, owner, repo string, runNum int, jobIDs map[string]int64, pipeline ci.Pipeline, jobs []string, workDir string, limit int) map[string]storage.JobStatus {
	type result struct {
		name string
		st   storage.JobStatus
	}
	results := make(chan result, len(jobs))
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for _, jn := range jobs {
		wg.Add(1)
		go func(jn string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			st := r.runJob(ctx, owner, repo, runNum, jn, jobIDs[jn], pipeline.Jobs[jn], workDir)
			results <- result{jn, st}
		}(jn)
	}
	wg.Wait()
	close(results)

	out := make(map[string]storage.JobStatus, len(jobs))
	for res := range results {
		out[res.name] = res.st
	}
	return out
}

// classifyJob decides a job's schedulability from its dependencies' statuses:
// ready when every need has succeeded, skip when any resolved need did not
// succeed (failed/skipped/errored), and neither (wait) when a need is still
// pending. ready and skip are mutually exclusive.
func classifyJob(needs []string, status map[string]storage.JobStatus) (ready, skip bool) {
	allResolvedSuccess := true
	for _, dep := range needs {
		st, resolved := status[dep]
		if !resolved {
			allResolvedSuccess = false
			continue
		}
		if st != storage.JobSuccess {
			return false, true
		}
	}
	return allResolvedSuccess, false
}

// emit appends an event, logging (not failing) on write error — a broken log
// file shouldn't crash the run.
func (r *ciRunner) emit(elog *ci.EventLog, eventType string, data map[string]any) {
	if _, err := elog.Append(eventType, data); err != nil {
		r.logger.Error("ci event append", "type", eventType, "err", err)
	}
}

// emitLines splits captured output into per-line events with 1-based line
// numbers. A trailing newline doesn't yield a spurious empty final line.
func emitLines(r *ciRunner, elog *ci.EventLog, stepID, eventType, stream, output string) {
	if output == "" {
		return
	}
	for i, line := range strings.Split(strings.TrimSuffix(output, "\n"), "\n") {
		r.emit(elog, eventType, map[string]any{
			"step_id": stepID, "stream": stream, "line": line, "line_number": i + 1,
		})
	}
}

func (r *ciRunner) finish(runID int64, status storage.RunStatus) {
	if err := storage.FinishRun(r.db, runID, status); err != nil {
		r.logger.Error("ci finish run", "run_id", runID, "status", status, "err", err)
	}
}

func (r *ciRunner) finishJob(jobID int64, status storage.JobStatus, exitCode *int) {
	if err := storage.FinishJob(r.db, jobID, status, exitCode); err != nil {
		r.logger.Error("ci finish job", "job_id", jobID, "status", status, "err", err)
	}
}

// hostSession runs a job's steps on the host via the injected stepExecutor (the
// legacy, non-isolated path; also the unit tests' seam). It owns no resources,
// so Close is a no-op.
type hostSession struct {
	workDir string
	exec    stepExecutor
}

func (h *hostSession) Exec(ctx context.Context, stepYAML string) (stepResult, error) {
	return h.exec(ctx, h.workDir, stepYAML)
}

func (h *hostSession) Close() error { return nil }

// dockerSession runs a job's steps inside a single throwaway container, keeping
// repo-authored commands off the host. The container is started detached
// (`sleep infinity`) at Open and torn down at Close; each step is a
// `docker exec mooncake step` into it, so steps share the bind-mounted
// workspace and the per-step JSON contract is identical to the host path.
type dockerSession struct {
	name   string
	logger *slog.Logger
}

// openDockerSession starts the per-job container. The workspace is bind-mounted
// at /work and the container runs as the moongitd uid:gid so files it writes
// stay owned by moongitd (root-owned files would break workspace cleanup). The
// image must be glibc-based and carry `mooncake` on PATH (see ci/Dockerfile).
func openDockerSession(ctx context.Context, logger *slog.Logger, name, workDir, image string) (jobSession, error) {
	args := []string{
		"run", "-d", "--rm",
		"--name", name,
		"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		"-v", workDir + ":/work",
		"-w", "/work",
		"--entrypoint", "sleep",
		image, "infinity",
	}
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker run %s: %v (%s)", image, err, strings.TrimSpace(string(out)))
	}
	return &dockerSession{name: name, logger: logger}, nil
}

func (d *dockerSession) Exec(ctx context.Context, stepYAML string) (stepResult, error) {
	cmd := exec.CommandContext(ctx, "docker", "exec", d.name, "mooncake", "step", stepYAML)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	return parseStepResult(ctx, stdout.Bytes(), stderr.Bytes(), runErr)
}

// Close removes the container. It uses a fresh background context with a short
// timeout so teardown still runs after a run-timeout has cancelled the parent
// context — otherwise the detached container would leak.
func (d *dockerSession) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "docker", "rm", "-f", d.name).CombinedOutput(); err != nil {
		d.logger.Error("ci container cleanup", "name", d.name, "err", err, "out", strings.TrimSpace(string(out)))
	}
	return nil
}

// sweepOrphanContainers removes any moongit-ci-* or moongit-agent-* containers
// left behind by a crashed runner. The two name filters are OR'd by docker, so
// both CI job containers and agent containers are reaped. Best-effort:
// failures are logged, not fatal.
func sweepOrphanContainers(ctx context.Context, logger *slog.Logger) {
	out, err := exec.CommandContext(ctx, "docker", "ps", "-aq",
		"--filter", "name=moongit-ci-", "--filter", "name=moongit-agent-").Output()
	if err != nil {
		logger.Warn("ci orphan container scan", "err", err)
		return
	}
	ids := strings.Fields(string(out))
	if len(ids) == 0 {
		return
	}
	if out, err := exec.CommandContext(ctx, "docker", append([]string{"rm", "-f"}, ids...)...).CombinedOutput(); err != nil {
		logger.Warn("ci orphan container sweep", "err", err, "out", strings.TrimSpace(string(out)))
		return
	}
	logger.Info("ci swept orphan containers", "count", len(ids))
}

// containerName builds a docker-safe, collision-free name for a job's
// container. jobID (a unique PK) guarantees uniqueness; the sanitized job name
// is appended for readability in `docker ps`.
func containerName(jobID int64, jobName string) string {
	return fmt.Sprintf("moongit-ci-%d-%s", jobID, sanitizeContainerName(jobName))
}

// sanitizeContainerName maps any character outside docker's name charset
// ([a-zA-Z0-9_.-]) to '-'.
func sanitizeContainerName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '.', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

// runMooncakeStep executes one translated step via `mooncake step '<YAML>'` in
// workDir. It backs the host session.
func runMooncakeStep(ctx context.Context, workDir, stepYAML string) (stepResult, error) {
	cmd := exec.CommandContext(ctx, "mooncake", "step", stepYAML)
	cmd.Dir = workDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	return parseStepResult(ctx, stdout.Bytes(), stderr.Bytes(), runErr)
}

// parseStepResult turns a `mooncake step` invocation's output into a stepResult.
// mooncake prints its JSON result to stdout even when the step fails and the
// process exits non-zero, so we parse stdout regardless of exit code and only
// treat an unparseable result (or a cancelled context) as an executor error.
func parseStepResult(ctx context.Context, stdout, stderr []byte, runErr error) (stepResult, error) {
	if ctx.Err() != nil {
		return stepResult{}, fmt.Errorf("step cancelled: %w", ctx.Err())
	}
	var res stepResult
	if jerr := json.Unmarshal(stdout, &res); jerr != nil {
		return stepResult{}, fmt.Errorf("mooncake step: %v (stderr: %s)", runErr, strings.TrimSpace(string(stderr)))
	}
	return res, nil
}

// gitCheckout materializes the repo tree at commitSHA into workDir via
// `git archive | tar -x` — a clean tree with no .git, which is all CI needs.
func gitCheckout(ctx context.Context, bareRepo, commitSHA, workDir string) error {
	archive := exec.CommandContext(ctx, "git", "--git-dir", bareRepo, "archive", "--format=tar", commitSHA)
	tar := exec.CommandContext(ctx, "tar", "-x", "-C", workDir)

	pipe, err := archive.StdoutPipe()
	if err != nil {
		return err
	}
	tar.Stdin = pipe
	var aerr, terr bytes.Buffer
	archive.Stderr = &aerr
	tar.Stderr = &terr

	if err := tar.Start(); err != nil {
		return err
	}
	if err := archive.Start(); err != nil {
		return err
	}
	// archive.Wait closes the pipe after git exits, giving tar its EOF.
	archiveErr := archive.Wait()
	tarErr := tar.Wait()
	if archiveErr != nil {
		return fmt.Errorf("git archive: %v (%s)", archiveErr, strings.TrimSpace(aerr.String()))
	}
	if tarErr != nil {
		return fmt.Errorf("tar extract: %v (%s)", tarErr, strings.TrimSpace(terr.String()))
	}
	return nil
}

// gitReadPipeline reads mgitci.yml at commitSHA from the bare repo. A missing
// file (git reports the path doesn't exist at that rev) yields ok=false rather
// than an error — that's the gate for "this commit has no pipeline".
func gitReadPipeline(bareRepo, commitSHA string) ([]byte, bool, error) {
	cmd := exec.Command("git", "--git-dir", bareRepo, "show", commitSHA+":mgitci.yml")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := stderr.String()
		if strings.Contains(msg, "does not exist") || strings.Contains(msg, "exists on disk, but not in") {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("git show: %v (%s)", err, strings.TrimSpace(msg))
	}
	return stdout.Bytes(), true, nil
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
