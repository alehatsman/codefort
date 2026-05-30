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
	"strconv"
	"strings"
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

// runCIRunner launches the in-process CI runner beside the reapers. One worker;
// concurrency caps are deferred (#26). Runs until ctx is cancelled.
func runCIRunner(ctx context.Context, db *sql.DB, cfg *config.Config, logger *slog.Logger) {
	newCIRunner(db, cfg, logger).run(ctx)
}

func (r *ciRunner) run(ctx context.Context) {
	interval := r.cfg.CIPollInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	r.logger.Info("ci runner started", "poll", interval, "run_timeout", r.cfg.CIRunTimeout, "isolation", r.cfg.CIIsolation)

	// A crashed runner can leave job containers behind; reap them before
	// taking new work so they don't accumulate.
	if r.cfg.CIIsolation == "docker" {
		sweepOrphanContainers(ctx, r.logger)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		// Drain every claimable run before sleeping.
		for {
			run, err := storage.ClaimNextRun(r.db, r.runLease())
			if errors.Is(err, storage.ErrNoRunQueued) {
				break
			}
			if err != nil {
				r.logger.Error("ci claim", "err", err)
				break
			}
			r.executeRun(ctx, run)
			if ctx.Err() != nil {
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
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

// executeRun runs one claimed run end-to-end: gate, checkout, per-job step
// execution emitting the event stream, status mirroring, and workspace
// cleanup.
func (r *ciRunner) executeRun(parent context.Context, run storage.CIRun) {
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
		job, err := storage.CreateJob(r.db, run.ID, jn)
		if err != nil {
			log.Error("ci create job", "job", jn, "err", err)
			r.finish(run.ID, storage.RunError)
			return
		}
		jobIDs[jn] = job.ID
	}

	// Execute jobs in topo order. A job whose dependency didn't succeed is
	// skipped. Sequential in v1; parallelism is #26.
	status := make(map[string]storage.JobStatus, len(order))
	anyNotSuccess := false
	for _, jn := range order {
		job := pipeline.Jobs[jn]
		if dep, unmet := firstUnsatisfied(job.Needs, status); unmet {
			log.Info("ci job skipped (dependency not satisfied)", "job", jn, "dep", dep)
			r.finishJob(jobIDs[jn], storage.JobSkipped, nil)
			status[jn] = storage.JobSkipped
			anyNotSuccess = true
			continue
		}
		st := r.runJob(ctx, owner, name, run.Number, jn, jobIDs[jn], job, workDir)
		status[jn] = st
		if st != storage.JobSuccess {
			anyNotSuccess = true
		}
		if ctx.Err() != nil {
			break // timeout / shutdown: stop launching further jobs
		}
	}

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
			"step_id": stepID, "action": step.Action, "global_step": i + 1,
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

// firstUnsatisfied returns the first dependency that did not finish
// successfully; a job runs only when all its needs succeeded.
func firstUnsatisfied(needs []string, status map[string]storage.JobStatus) (string, bool) {
	for _, dep := range needs {
		if status[dep] != storage.JobSuccess {
			return dep, true
		}
	}
	return "", false
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

// sweepOrphanContainers removes any moongit-ci-* containers left behind by a
// crashed runner. Best-effort: failures are logged, not fatal.
func sweepOrphanContainers(ctx context.Context, logger *slog.Logger) {
	out, err := exec.CommandContext(ctx, "docker", "ps", "-aq", "--filter", "name=moongit-ci-").Output()
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
