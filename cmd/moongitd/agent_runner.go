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

	st := r.runAgentJob(ctx, owner, name, run.ID, run.Number, job.ID, workDir, issue)

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

// runAgentJob opens the agent's container and drives one turn of the claude
// session, streaming its stream-json output onto the job's events.jsonl so the
// existing SSE endpoint + run viewer render the live transcript with no
// transport changes. It composes the launch context (system prompt + turn-1
// issue prompt), runs `claude -p … --output-format stream-json` in the
// container, translates each line into an agent event, and maps claude's
// terminal result / exit code onto the run status.
//
// This is turn 1 only (the single-turn slice of #76). The awaiting_input turn
// loop — follow-up turns via POST /runs/{id}/turns onto the same session, and
// the idle reaper — is the next slice; dex grounding + scoped creds layer in
// via #77 / dex#6.
func (r *ciRunner) runAgentJob(ctx context.Context, owner, repo string, runID int64, runNum int, jobID int64, workDir string, issue api.Issue) storage.JobStatus {
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

	// Open the agent's container (the same isolation seam CI jobs use). A
	// failure here — image missing, docker down — fails the run loudly.
	sess, err := r.newSession(ctx, agentContainerName(jobID), workDir, r.cfg.AgentDefaultImage)
	if err != nil {
		r.emit(elog, ci.EventAgentRaw, map[string]any{"line": err.Error()})
		r.emit(elog, ci.EventRunFailed, map[string]any{"error": err.Error()})
		r.finishJob(jobID, storage.JobError, nil)
		log.Error("agent open session", "image", r.cfg.AgentDefaultImage, "err", err)
		return storage.JobError
	}
	defer sess.Close()

	stream, ok := sess.(streamingSession)
	if !ok {
		// docker + host sessions both implement it; a session that doesn't
		// can't run an agent.
		msg := "agent session does not support streaming exec"
		r.emit(elog, ci.EventRunFailed, map[string]any{"error": msg})
		r.finishJob(jobID, storage.JobError, nil)
		log.Error(msg)
		return storage.JobError
	}

	// Compose and run turn 1: the issue body as the user message, with the
	// moongit-authored system prompt for role/scope/safety.
	systemPrompt := composeAgentSystemPrompt(owner, repo, issue)
	turnPrompt := composeTurnPrompt(issue)
	argv := buildClaudeArgv(agentSessionID(runID), turnPrompt, systemPrompt, false)

	r.emit(elog, ci.EventAgentTurnStarted, map[string]any{"turn": 1, "prompt": turnPrompt})

	var result *claudeResult
	exitCode, execErr := stream.ExecStream(ctx, argv, func(line []byte) {
		et, data, res := translateClaudeLine(line)
		if et == "" {
			return // blank line
		}
		r.emit(elog, et, data)
		if res != nil {
			result = res
		}
	})

	status := turnStatus(result, exitCode, execErr)
	turnData := map[string]any{"turn": 1, "status": status}
	if result != nil {
		turnData["num_turns"] = result.NumTurns
		turnData["duration_ms"] = result.DurationMS
		turnData["cost_usd"] = result.TotalCostUSD
	}
	r.emit(elog, ci.EventAgentTurnCompleted, turnData)

	// execErr is a real executor failure (couldn't run claude, or ctx cancel),
	// distinct from claude running and reporting an error result.
	if execErr != nil {
		r.emit(elog, ci.EventRunFailed, map[string]any{"error": execErr.Error()})
		r.finishJob(jobID, storage.JobError, nil)
		log.Error("agent turn exec", "err", execErr)
		return storage.JobError
	}
	if status != "success" {
		r.emit(elog, ci.EventRunFailed, map[string]any{"status": status})
		ec := exitCode
		r.finishJob(jobID, storage.JobFailed, &ec)
		log.Info("agent turn not successful", "status", status, "exit", exitCode)
		return storage.JobFailed
	}

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
