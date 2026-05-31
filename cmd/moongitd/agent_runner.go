package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/alehatsman/moongit/internal/api"
	"github.com/alehatsman/moongit/internal/ci"
	"github.com/alehatsman/moongit/internal/storage"
)

// executeAgentRun runs one claimed agent run end-to-end. It mirrors
// executeRun's CI path — resolve repo, fresh workspace, checkout, run the work
// in an isolated container, finalize — but instead of a translated mgitci.yml
// it works the issue the run serves via a single synthetic "agent" job. The
// job's body (the actual Claude turn loop) is the executor seam #76 fills;
// #74 lands everything around it so the lifecycle is real.
func (r *ciRunner) executeAgentRun(parent context.Context, run storage.CIRun) {
	log := r.logger.With("kind", "agent", "run_id", run.ID, "run", run.Number, "sha", short(run.CommitSHA))

	owner, name, err := storage.RepoIdent(r.db, run.RepoID)
	if err != nil {
		log.Error("agent resolve repo", "err", err)
		r.finish(run.ID, storage.RunError)
		return
	}
	log = log.With("repo", owner+"/"+name)

	// Gate: an agent run must serve a real issue. Unlike CI there's no
	// mgitci.yml requirement — the issue is the work.
	if run.IssueNumber == nil {
		log.Error("agent run has no issue")
		r.finish(run.ID, storage.RunError)
		return
	}
	issue, err := storage.GetIssue(r.db, run.RepoID, *run.IssueNumber)
	if err != nil {
		log.Error("agent get issue", "issue", *run.IssueNumber, "err", err)
		r.finish(run.ID, storage.RunError)
		return
	}
	log = log.With("issue", issue.Number)

	// Fresh workspace; always cleaned up. Kept under a separate "agent" subtree
	// from CI's so the two never collide on run id.
	workDir := filepath.Join(r.cfg.DataDir, "agent", "work", strconv.FormatInt(run.ID, 10))
	if err := os.RemoveAll(workDir); err == nil {
		err = os.MkdirAll(workDir, 0o755)
	}
	if err != nil {
		log.Error("agent workspace", "err", err)
		r.finish(run.ID, storage.RunError)
		return
	}
	defer os.RemoveAll(workDir)

	ctx := parent
	if r.cfg.AgentRunTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(parent, r.cfg.AgentRunTimeout)
		defer cancel()
	}

	if err := r.checkout(ctx, filepath.Join(r.cfg.ReposDir, owner, name+".git"), run.CommitSHA, workDir); err != nil {
		log.Error("agent checkout", "err", err)
		r.finish(run.ID, storage.RunError)
		return
	}

	// One synthetic job carries the agent session, so the run-detail UI, event
	// stream, and retention all treat it exactly like a CI job.
	job, err := storage.CreateJob(r.db, run.ID, agentJobName, nil)
	if err != nil {
		log.Error("agent create job", "err", err)
		r.finish(run.ID, storage.RunError)
		return
	}

	st := r.runAgentJob(ctx, owner, name, run.Number, job.ID, workDir, issue)

	final := storage.RunSuccess
	switch {
	case ctx.Err() != nil:
		final = storage.RunError
	case st != storage.JobSuccess:
		final = storage.RunFailed
	}
	r.finish(run.ID, final)
	log.Info("agent run finished", "status", final)
}

// agentJobName is the single job every agent run carries.
const agentJobName = "agent"

// runAgentJob opens the agent's container and drives its work, emitting the
// mooncake-shaped event stream into the job's events.jsonl so the existing SSE
// + run-detail UI render it unchanged.
//
// The container body — launching the Claude CLI, composing the system prompt +
// dex repo summary + MCP config, streaming stream-json turns onto this event
// log, and the awaiting_input turn loop — is the executor seam tracked by #76.
// #74 stops once the container is up and the seam is reached; it emits a single
// placeholder step so a spawned run renders as a finished job today.
func (r *ciRunner) runAgentJob(ctx context.Context, owner, repo string, runNum int, jobID int64, workDir string, issue api.Issue) storage.JobStatus {
	log := r.logger.With("kind", "agent", "run", runNum, "issue", issue.Number)

	elog, err := ci.OpenEventLog(r.cfg.DataDir, owner, repo, runNum, agentJobName)
	if err != nil {
		log.Error("agent open event log", "err", err)
		r.finishJob(jobID, storage.JobError, nil)
		return storage.JobError
	}
	defer elog.Close()

	if err := storage.StartJob(r.db, jobID); err != nil {
		log.Error("agent start job", "err", err)
	}
	r.emit(elog, ci.EventRunStarted, map[string]any{"total_steps": 1})
	r.emit(elog, ci.EventPlanLoaded, map[string]any{"total_steps": 1})

	// Open the agent's container (the same isolation seam CI jobs use). A
	// failure here — image missing, docker down — fails the run loudly.
	sess, err := r.newSession(ctx, agentContainerName(jobID), workDir, r.cfg.AgentDefaultImage)
	if err != nil {
		r.emit(elog, ci.EventStepStderr, map[string]any{
			"step_id": "session", "stream": "stderr", "line": err.Error(), "line_number": 1,
		})
		r.emit(elog, ci.EventRunFailed, map[string]any{"error": err.Error()})
		r.finishJob(jobID, storage.JobError, nil)
		log.Error("agent open session", "image", r.cfg.AgentDefaultImage, "err", err)
		return storage.JobError
	}
	defer sess.Close()

	// --- executor seam (#76) ---------------------------------------------
	// The Claude turn loop lands here: launch `claude` in the container with
	// the composed launch context, stream stream-json turns onto elog, and
	// hold the container alive in awaiting_input between human turns. For now
	// emit one synthetic step so the run is a complete, renderable job.
	const stepID = "step-0001"
	r.emit(elog, ci.EventStepStarted, map[string]any{
		"step_id": stepID, "action": "agent", "name": "spawn claude agent", "global_step": 1,
	})
	line := fmt.Sprintf("agent container ready for issue #%d (%q); claude turn loop wired in #76", issue.Number, issue.Title)
	r.emit(elog, ci.EventStepStdout, map[string]any{
		"step_id": stepID, "stream": "stdout", "line": line, "line_number": 1,
	})
	r.emit(elog, ci.EventStepCompleted, map[string]any{
		"step_id": stepID, "duration_ms": 0, "changed": false,
		"result": map[string]any{"rc": 0, "failed": false, "status": "ok"},
	})
	// ---------------------------------------------------------------------

	r.emit(elog, ci.EventRunCompleted, map[string]any{"total_steps": 1})
	zero := 0
	r.finishJob(jobID, storage.JobSuccess, &zero)
	return storage.JobSuccess
}

// agentContainerName builds a docker-safe, collision-free name for an agent
// run's container, distinct from CI's "moongit-ci-" prefix so the orphan sweep
// can tell them apart while still reaping both.
func agentContainerName(jobID int64) string {
	return fmt.Sprintf("moongit-agent-%d", jobID)
}
