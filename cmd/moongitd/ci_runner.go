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

// The runner's three external boundaries, injected so the orchestration is
// testable without the real git / mooncake binaries.
type (
	// stepExecutor runs one mooncake step (YAML) in workDir.
	stepExecutor func(ctx context.Context, workDir, stepYAML string) (stepResult, error)
	// checkoutFunc materializes the repo tree at commitSHA into workDir.
	checkoutFunc func(ctx context.Context, bareRepo, commitSHA, workDir string) error
	// pipelineReader reads mgitci.yml at commitSHA; ok=false means absent.
	pipelineReader func(bareRepo, commitSHA string) (raw []byte, ok bool, err error)
)

// ciRunner executes queued CI runs in-process, emitting the mooncake-shaped
// event stream per job. Everything downstream (storage status, API, UI)
// consumes that stream, so an agentd-backed runner can later replace this one
// behind the same boundary (#26).
type ciRunner struct {
	db     *sql.DB
	cfg    *config.Config
	logger *slog.Logger

	exec         stepExecutor
	checkout     checkoutFunc
	readPipeline pipelineReader
}

func newCIRunner(db *sql.DB, cfg *config.Config, logger *slog.Logger) *ciRunner {
	return &ciRunner{
		db:           db,
		cfg:          cfg,
		logger:       logger,
		exec:         runMooncakeStep,
		checkout:     gitCheckout,
		readPipeline: gitReadPipeline,
	}
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
	r.logger.Info("ci runner started", "poll", interval, "run_timeout", r.cfg.CIRunTimeout)

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

	for i, step := range steps {
		stepID := fmt.Sprintf("step-%04d", i+1)
		r.emit(elog, ci.EventStepStarted, map[string]any{
			"step_id": stepID, "action": step.Action, "global_step": i + 1,
		})

		res, execErr := r.exec(ctx, workDir, step.YAML)
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

// runMooncakeStep executes one translated step via `mooncake step '<YAML>'` in
// workDir. mooncake prints its JSON result to stdout even when the step fails
// and the process exits non-zero, so we parse stdout regardless of exit code
// and only treat an unparseable result (or a cancelled context) as an
// executor error.
func runMooncakeStep(ctx context.Context, workDir, stepYAML string) (stepResult, error) {
	cmd := exec.CommandContext(ctx, "mooncake", "step", stepYAML)
	cmd.Dir = workDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	if ctx.Err() != nil {
		return stepResult{}, fmt.Errorf("step cancelled: %w", ctx.Err())
	}
	var res stepResult
	if jerr := json.Unmarshal(stdout.Bytes(), &res); jerr != nil {
		return stepResult{}, fmt.Errorf("mooncake step: %v (stderr: %s)", runErr, strings.TrimSpace(stderr.String()))
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
