package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/alehatsman/moongit/internal/ci"
	"github.com/alehatsman/moongit/internal/storage"
)

// agentJobName is the single job every agent run carries.
const agentJobName = "agent"

// executeAgentRun runs turn 1 of a claimed agent run: resolve repo, fresh
// workspace, checkout, open the container, and run the issue body as the first
// claude turn. On success the run is *parked* in awaiting_input with its
// container left running — the turn dispatcher resumes it for follow-up turns,
// and the reaper / an explicit finish tears it down. Only an infrastructure
// failure (bad checkout, container won't open, exec died) finalizes the run
// here, cleaning up as it goes.
func (r *ciRunner) executeAgentRun(parent context.Context, run storage.CIRun) {
	log := r.logger.With("kind", "agent", "run_id", run.ID, "run", run.Number, "sha", short(run.CommitSHA))

	owner, name, err := storage.RepoIdent(r.db, run.RepoID)
	if err != nil {
		log.Error("agent resolve repo", "err", err)
		r.finish(run, storage.RunError)
		return
	}
	log = log.With("repo", owner+"/"+name)

	// Gate: an agent run must serve a real issue — the issue is the work.
	if run.IssueNumber == nil {
		log.Error("agent run has no issue")
		r.finish(run, storage.RunError)
		return
	}
	issue, err := storage.GetIssue(r.db, run.RepoID, *run.IssueNumber)
	if err != nil {
		log.Error("agent get issue", "issue", *run.IssueNumber, "err", err)
		r.finish(run, storage.RunError)
		return
	}
	log = log.With("issue", issue.Number)

	// Select the execution model up front so an unknown model fails before
	// we spend a workspace + container on it (#110).
	exec, err := newAgentExecutor(run.ExecutionModel, r.cfg, run.PilotAllowShell)
	if err != nil {
		log.Error("agent executor", "model", run.ExecutionModel, "err", err)
		r.finish(run, storage.RunError)
		return
	}
	log = log.With("model", exec.Model())

	workDir := agentWorkDir(r.cfg.DataDir, run.ID)
	if err := os.RemoveAll(workDir); err == nil {
		err = os.MkdirAll(workDir, 0o755)
	}
	if err != nil {
		log.Error("agent workspace", "err", err)
		r.finish(run, storage.RunError)
		return
	}

	if err := r.checkout(parent, filepath.Join(r.cfg.ReposDir, owner, name+".git"), run.CommitSHA, workDir); err != nil {
		log.Error("agent checkout", "err", err)
		os.RemoveAll(workDir)
		r.finish(run, storage.RunError)
		return
	}

	// The checkout is a git-archive extract with no .git. The mooncake-pilot
	// model runs `git` (snapshot/diff) inside /work, so make it a real repo at
	// the base commit; claude-edit just ignores it. Best-effort — a failure
	// here only matters for pilot, which will surface its own error.
	if err := initAgentGitRepo(parent, workDir); err != nil {
		log.Warn("agent git init", "err", err)
	}

	// One synthetic job carries the whole agent session, so the run-detail UI,
	// event stream, and retention treat it like a CI job. It stays 'running'
	// across turns and is finalized only when the session ends.
	job, err := storage.CreateJob(r.db, run.ID, agentJobName, nil)
	if err != nil {
		log.Error("agent create job", "err", err)
		os.RemoveAll(workDir)
		r.finish(run, storage.RunError)
		return
	}
	if err := storage.StartJob(r.db, job.ID); err != nil {
		log.Error("agent start job", "err", err)
	}

	elog, err := ci.OpenEventLog(r.cfg.DataDir, owner, name, run.Number, agentJobName)
	if err != nil {
		log.Error("agent open event log", "err", err)
		r.failAgentRun(run.ID, job.ID, workDir)
		return
	}
	r.emit(elog, ci.EventRunStarted, map[string]any{"total_steps": 1})

	// Mint the ephemeral, per-run moongit token and compose the container env
	// (creds, scoped token, dex wiring). The token is revoked on teardown.
	moongitToken, err := storage.GenerateTokenString()
	if err == nil {
		_, err = storage.CreateToken(r.db, agentTokenName(run.ID), moongitToken)
	}
	if err != nil {
		r.emit(elog, ci.EventRunFailed, map[string]any{"error": "mint agent token: " + err.Error()})
		elog.Close()
		log.Error("agent mint token", "err", err)
		r.failAgentRun(run.ID, job.ID, workDir)
		return
	}
	// Effective Claude token: operator-set Settings value wins over the env
	// (#106), so spawning works without a moongitd restart.
	claudeToken := storage.SettingValue(r.db, storage.SettingAgentClaudeToken)
	env := agentContainerEnv(r.cfg, claudeToken, moongitToken, agentServerURL(r.cfg))

	// Generate the dex MCP config (if dex is configured) into the workspace.
	mcpPath, _, err := writeDexMCPConfig(workDir, r.cfg)
	if err != nil {
		log.Error("agent write mcp config", "err", err)
		mcpPath = "" // non-fatal: run without dex MCP
	}

	// Open the container (the same isolation seam CI jobs use) with the env
	// injected. A failure here — image missing, docker down — fails the run
	// loudly. NOTE: we do not Close the session on the happy path; the
	// container must outlive this goroutine for follow-up turns to resume.
	sess, err := r.newAgentSession(parent, agentContainerName(job.ID), workDir, r.cfg.AgentDefaultImage, env)
	if err != nil {
		r.emit(elog, ci.EventAgentRaw, map[string]any{"line": err.Error()})
		r.emit(elog, ci.EventRunFailed, map[string]any{"error": err.Error()})
		elog.Close()
		log.Error("agent open session", "image", r.cfg.AgentDefaultImage, "err", err)
		r.failAgentRun(run.ID, job.ID, workDir)
		return
	}
	stream, ok := sess.(streamingSession)
	if !ok {
		r.emit(elog, ci.EventRunFailed, map[string]any{"error": "session does not support streaming exec"})
		elog.Close()
		r.failAgentRun(run.ID, job.ID, workDir)
		return
	}

	spec := turnSpec{
		sessionID:    agentSessionID(run.ID),
		prompt:       composeTurnPrompt(issue),
		systemPrompt: composeAgentSystemPrompt(owner, name, issue),
		mcpPath:      mcpPath,
		resume:       false,
	}
	status, execErr := r.runAgentTurn(parent, stream, elog, exec, 1, spec)
	elog.Close()

	if execErr != nil {
		// Infra failure (couldn't run claude / ctx cancel): the container is
		// likely unusable — finalize and tear down.
		log.Error("agent turn 1 exec", "err", execErr)
		r.commentAgentFailure(run, "the first turn failed to run")
		r.failAgentRun(run.ID, job.ID, workDir)
		return
	}
	// Turn 1 ran (success, or claude reported an error result the human can
	// course-correct): park awaiting input, leaving the container alive.
	if err := storage.MarkRunAwaitingInput(r.db, run.ID); err != nil {
		log.Error("agent park awaiting_input", "err", err)
	}
	log.Info("agent run awaiting input", "turn1_status", status)
}

// dispatchTurn runs one claimed follow-up turn on a parked run: resume the
// claude session inside the still-running container, stream onto the same event
// log, and re-park awaiting input. An exec failure finalizes the run.
func (r *ciRunner) dispatchTurn(parent context.Context, turn storage.AgentTurn, run storage.CIRun) {
	log := r.logger.With("kind", "agent", "run", run.Number, "turn", turn.Seq)

	owner, name, err := storage.RepoIdent(r.db, run.RepoID)
	if err != nil {
		log.Error("agent turn resolve repo", "err", err)
		storage.FinishTurn(r.db, turn.ID, storage.TurnError)
		r.finish(run, storage.RunError)
		return
	}
	jobID, ok := r.agentJobID(run.ID)
	if !ok {
		log.Error("agent turn: no agent job for run")
		storage.FinishTurn(r.db, turn.ID, storage.TurnError)
		r.failAgentRun(run.ID, 0, agentWorkDir(r.cfg.DataDir, run.ID))
		return
	}

	elog, err := ci.OpenEventLog(r.cfg.DataDir, owner, name, run.Number, agentJobName)
	if err != nil {
		log.Error("agent turn open event log", "err", err)
		storage.FinishTurn(r.db, turn.ID, storage.TurnError)
		return
	}

	sess, err := r.attachSession(parent, agentContainerName(jobID))
	if err != nil {
		r.emit(elog, ci.EventRunFailed, map[string]any{"error": err.Error()})
		elog.Close()
		storage.FinishTurn(r.db, turn.ID, storage.TurnError)
		log.Error("agent turn attach", "err", err)
		r.failAgentRun(run.ID, jobID, agentWorkDir(r.cfg.DataDir, run.ID))
		return
	}
	stream, ok := sess.(streamingSession)
	if !ok {
		r.emit(elog, ci.EventRunFailed, map[string]any{"error": "session does not support streaming exec"})
		elog.Close()
		storage.FinishTurn(r.db, turn.ID, storage.TurnError)
		r.failAgentRun(run.ID, jobID, agentWorkDir(r.cfg.DataDir, run.ID))
		return
	}

	exec, err := newAgentExecutor(run.ExecutionModel, r.cfg, run.PilotAllowShell)
	if err != nil {
		r.emit(elog, ci.EventRunFailed, map[string]any{"error": err.Error()})
		elog.Close()
		storage.FinishTurn(r.db, turn.ID, storage.TurnError)
		log.Error("agent turn executor", "model", run.ExecutionModel, "err", err)
		r.failAgentRun(run.ID, jobID, agentWorkDir(r.cfg.DataDir, run.ID))
		return
	}

	// turn.Seq is the follow-up index (1-based); display number accounts for
	// turn 1 being the issue body. Resume the session (claude); the system
	// prompt is already in it, so it's omitted. The MCP config file persists
	// in the workspace from turn 1.
	mcpPath := ""
	if r.cfg.DexURL != "" {
		mcpPath = "/work/" + dexMCPConfigName
	}
	spec := turnSpec{
		sessionID: agentSessionID(run.ID),
		prompt:    turn.Body,
		mcpPath:   mcpPath,
		resume:    true,
	}
	_, execErr := r.runAgentTurn(parent, stream, elog, exec, turn.Seq+1, spec)
	elog.Close()

	if execErr != nil {
		log.Error("agent turn exec", "err", execErr)
		storage.FinishTurn(r.db, turn.ID, storage.TurnError)
		r.commentAgentFailure(run, fmt.Sprintf("turn %d failed to run", turn.Seq+1))
		r.failAgentRun(run.ID, jobID, agentWorkDir(r.cfg.DataDir, run.ID))
		return
	}
	if err := storage.FinishTurn(r.db, turn.ID, storage.TurnDone); err != nil {
		log.Error("agent finish turn", "err", err)
	}
	if err := storage.MarkRunAwaitingInput(r.db, run.ID); err != nil {
		log.Error("agent re-park awaiting_input", "err", err)
	}
	log.Info("agent turn done; awaiting input")
}

// runAgentTurn composes and runs one turn under a per-turn deadline via the
// run's executor, streaming each translated output line onto the event log as
// an agent event and bracketing the turn with turn.started/completed. It
// returns the turn status and any executor error (couldn't run the CLI /
// cancelled), but does not itself finalize the run or job — the caller decides
// whether to park or fail. The executor decides what runs (claude vs mooncake
// pilot) and how to translate its output.
func (r *ciRunner) runAgentTurn(parent context.Context, stream streamingSession, elog *ci.EventLog, exec agentExecutor, turnNum int, spec turnSpec) (status string, execErr error) {
	ctx := parent
	if r.cfg.AgentTurnTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(parent, r.cfg.AgentTurnTimeout)
		defer cancel()
	}

	argv := exec.Argv(spec)
	r.emit(elog, ci.EventAgentTurnStarted, map[string]any{"turn": turnNum, "prompt": spec.prompt, "model": exec.Model()})

	var result *turnResult
	var stderrLines []string
	exitCode, execErr := stream.ExecStream(ctx, argv, func(line []byte) {
		et, data, res := exec.Translate(line)
		if et == "" {
			return
		}
		r.emit(elog, et, data)
		if res != nil {
			result = res
		}
	}, func(line []byte) {
		if s := strings.TrimRight(string(line), "\r\n"); s != "" {
			stderrLines = append(stderrLines, s)
		}
	})

	status = turnStatus(result, exitCode, execErr)
	// A failed turn must never be blank. The executor translates the tool's
	// stdout; a tool that writes its diagnostics to stderr (mooncake's planner
	// errors, a crash trace) would otherwise leave nothing in the transcript to
	// explain the failure. On any non-success turn, replay that stderr as
	// agent.raw so the operator can see why (#117); the happy path stays clean.
	if status != "success" {
		for _, line := range stderrLines {
			r.emit(elog, ci.EventAgentRaw, map[string]any{"line": line})
		}
	}
	turnData := map[string]any{"turn": turnNum, "status": status}
	if result != nil {
		turnData["num_turns"] = result.NumTurns
		turnData["duration_ms"] = result.DurationMS
		turnData["cost_usd"] = result.TotalCostUSD
	}
	r.emit(elog, ci.EventAgentTurnCompleted, turnData)
	return status, execErr
}

// reapExpiredAgents finalizes agent runs parked past their lifetime cap, tearing
// down the container they were holding so an abandoned session can't hold
// resources forever.
func (r *ciRunner) reapExpiredAgents(ctx context.Context) {
	runs, err := storage.ListExpiredAwaitingRuns(r.db, r.cfg.AgentRunTimeout)
	if err != nil {
		r.logger.Error("agent reap list", "err", err)
		return
	}
	for _, run := range runs {
		if ctx.Err() != nil {
			return
		}
		log := r.logger.With("kind", "agent", "run", run.Number)
		owner, name, err := storage.RepoIdent(r.db, run.RepoID)
		if err == nil {
			if elog, e := ci.OpenEventLog(r.cfg.DataDir, owner, name, run.Number, agentJobName); e == nil {
				r.emit(elog, ci.EventRunCompleted, map[string]any{"reason": "idle timeout"})
				elog.Close()
			}
		}
		jobID, _ := r.agentJobID(run.ID)
		r.tearDownAgent(run.ID, jobID, agentWorkDir(r.cfg.DataDir, run.ID))
		zero := 0
		r.finishJob(jobID, storage.JobSuccess, &zero)
		r.finish(run, storage.RunCanceled)
		r.commentAgentFailure(run, "idle/lifetime timeout")
		log.Info("agent run reaped (lifetime cap)")
	}
}

// failAgentRun finalizes a broken agent run: tear down its container +
// workspace + token and mark the job and run errored.
func (r *ciRunner) failAgentRun(runID, jobID int64, workDir string) {
	r.tearDownAgent(runID, jobID, workDir)
	if jobID != 0 {
		r.finishJob(jobID, storage.JobError, nil)
	}
	// Agent runs don't surface on the CI feed (finish skips them by Kind), so a
	// minimal run carrying just the id + kind is all finish needs here.
	r.finish(storage.CIRun{ID: runID, Kind: storage.RunKindAgent}, storage.RunError)
}

// tearDownAgent releases a finished agent run's resources: remove the container
// (best-effort), revoke its ephemeral moongit token, and delete the workspace.
func (r *ciRunner) tearDownAgent(runID, jobID int64, workDir string) {
	if jobID != 0 && r.teardownContainer != nil {
		r.teardownContainer(agentContainerName(jobID))
	}
	if err := storage.RevokeToken(r.db, agentTokenName(runID)); err != nil && !errors.Is(err, storage.ErrNotFound) {
		r.logger.Error("agent revoke token", "run_id", runID, "err", err)
	}
	if workDir != "" {
		os.RemoveAll(workDir)
	}
}

// agentJobID returns the id of a run's single "agent" job.
func (r *ciRunner) agentJobID(runID int64) (int64, bool) {
	jobs, err := storage.ListJobs(r.db, runID)
	if err != nil {
		r.logger.Error("agent list jobs", "run_id", runID, "err", err)
		return 0, false
	}
	for _, j := range jobs {
		if j.Name == agentJobName {
			return j.ID, true
		}
	}
	return 0, false
}

// agentWorkDir is the deterministic workspace path for an agent run, kept under
// a separate subtree from CI's so the two never collide on run id.
func agentWorkDir(dataDir string, runID int64) string {
	return filepath.Join(dataDir, "agent", "work", strconv.FormatInt(runID, 10))
}

// agentContainerName builds a docker-safe, collision-free name for an agent
// run's container, distinct from CI's "moongit-ci-" prefix so the orphan sweep
// can tell them apart while still reaping both.
func agentContainerName(jobID int64) string {
	return fmt.Sprintf("moongit-agent-%d", jobID)
}
