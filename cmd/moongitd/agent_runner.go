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
	exec, err := newAgentExecutor(run.ExecutionModel, r.cfg, run.MooncakeAllowShell)
	if err != nil {
		log.Error("agent executor", "model", run.ExecutionModel, "err", err)
		r.finish(run, storage.RunError)
		return
	}
	log = log.With("model", exec.Model())

	workDir := agentWorkDir(r.cfg.DataDir, run.ID)
	// Assign (not :=) so a RemoveAll/MkdirAll failure is seen by the err check
	// below — a shadowed inner err here used to swallow both.
	if err = os.RemoveAll(workDir); err == nil {
		err = os.MkdirAll(workDir, 0o755)
	}
	if err != nil {
		log.Error("agent workspace", "err", err)
		r.finish(run, storage.RunError)
		return
	}

	if err := r.checkout(parent, filepath.Join(r.cfg.ReposDir, owner, name+".git"), run.CommitSHA, workDir); err != nil {
		log.Error("agent checkout", "err", err)
		_ = os.RemoveAll(workDir)
		r.finish(run, storage.RunError)
		return
	}

	// gitCheckout cloned the bare repo into /work, so it's already a real repo
	// (history detached at the base commit) that the mooncake-agent model's git
	// snapshot/diff steps run against; claude-edit ignores it. Clone's `origin`
	// is the bare repo's local path, which mgit can't parse, so wire a `moongit`
	// remote at the server URL — mgit prefers it over origin — letting the agent
	// claim/comment/set-state on its issue (#144). Best-effort: a failure here
	// only matters for the mooncake/mgit path, which surfaces its own error.
	moongitURL := agentServerURL(r.cfg) + "/" + owner + "/" + name + ".git"
	if err := wireAgentMoongitRemote(parent, workDir, moongitURL); err != nil {
		log.Warn("agent wire moongit remote", "err", err)
	}

	// One synthetic job carries the whole agent session, so the run-detail UI,
	// event stream, and retention treat it like a CI job. It stays 'running'
	// across turns and is finalized only when the session ends.
	job, err := storage.CreateJob(r.db, run.ID, agentJobName, nil)
	if err != nil {
		log.Error("agent create job", "err", err)
		_ = os.RemoveAll(workDir)
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
	// Operator-set Settings values win over the env (#106), so spawning works
	// without a moongitd restart — the Claude token, the LLM base URL, and the
	// gateway auth token all override their MOONGIT_AGENT_* env counterparts.
	override := agentSettingsOverride{
		claudeToken:        storage.SettingValue(r.db, storage.SettingAgentClaudeToken),
		llmBaseURL:         storage.SettingValue(r.db, storage.SettingAgentLLMBaseURL),
		anthropicAuthToken: storage.SettingValue(r.db, storage.SettingAgentAnthropicAuthToken),
	}
	env := agentContainerEnv(r.cfg, override, moongitToken, agentServerURL(r.cfg))

	// Generate the agent MCP config (mgit always, dex when configured) into the
	// workspace.
	mcpPath, err := writeAgentMCPConfig(workDir, r.cfg, run.ToolProfile)
	if err != nil {
		log.Error("agent write mcp config", "err", err)
		mcpPath = "" // non-fatal: run without MCP servers
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

	in := turnInput{
		sessionID: agentSessionID(run.ID),
		owner:     owner,
		repo:      name,
		issue:     issue,
		firstTurn: true,
		mcpPath:   mcpPath,
	}
	turnCtx, h := r.registerAgentTurn(parent, run.ID)
	status, execErr := r.runAgentTurn(turnCtx, stream, elog, exec, 1, in)
	r.unregisterAgentTurn(run.ID, h)
	elog.Close()

	if h.canceled.Load() {
		// Operator force-stopped this turn (#146): CancelAgentRun already marked
		// the run RunCanceled and tore down. Don't re-finalize as an infra error.
		log.Info("agent run canceled by operator", "turn", 1)
		return
	}
	if execErr != nil {
		// Infra failure (couldn't run claude / ctx cancel): the container is
		// likely unusable — finalize and tear down.
		log.Error("agent turn 1 exec", "err", execErr)
		r.commentAgentFailure(run, "the first turn failed to run")
		r.failAgentRun(run.ID, job.ID, workDir)
		return
	}
	// The turn ran to completion. Whatever its verdict — success, a soft stop
	// (converged / no_progress), or a genuine step failure — park awaiting_input
	// and leave the container alive so the operator can click Continue and
	// resume from the persisted /work. Only an operator Stop (#146) or infra
	// death (execErr, above) ends a session; a non-fatal stop must not lose it
	// (#178, reversing the terminal stall/fail of #173/#145). The per-turn
	// verdict rides the turn.completed event + stderr replay (#117), so the
	// transcript still shows what happened.
	if err := storage.MarkRunAwaitingInput(r.db, run.ID); err != nil {
		// The park write failed, so the run stays 'running' with a live container
		// the idle reaper (awaiting_input only) will never touch. Tear it down
		// rather than strand the container until the next restart sweep.
		log.Error("agent park awaiting_input", "err", err)
		r.failAgentRun(run.ID, job.ID, workDir)
		return
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
		_ = storage.FinishTurn(r.db, turn.ID, storage.TurnError)
		r.finish(run, storage.RunError)
		return
	}
	jobID, ok := r.agentJobID(run.ID)
	if !ok {
		log.Error("agent turn: no agent job for run")
		_ = storage.FinishTurn(r.db, turn.ID, storage.TurnError)
		r.failAgentRun(run.ID, 0, agentWorkDir(r.cfg.DataDir, run.ID))
		return
	}

	elog, err := ci.OpenEventLog(r.cfg.DataDir, owner, name, run.Number, agentJobName)
	if err != nil {
		log.Error("agent turn open event log", "err", err)
		_ = storage.FinishTurn(r.db, turn.ID, storage.TurnError)
		return
	}

	sess, err := r.attachSession(parent, agentContainerName(jobID))
	if err != nil {
		r.emit(elog, ci.EventRunFailed, map[string]any{"error": err.Error()})
		elog.Close()
		_ = storage.FinishTurn(r.db, turn.ID, storage.TurnError)
		log.Error("agent turn attach", "err", err)
		r.failAgentRun(run.ID, jobID, agentWorkDir(r.cfg.DataDir, run.ID))
		return
	}
	stream, ok := sess.(streamingSession)
	if !ok {
		r.emit(elog, ci.EventRunFailed, map[string]any{"error": "session does not support streaming exec"})
		elog.Close()
		_ = storage.FinishTurn(r.db, turn.ID, storage.TurnError)
		r.failAgentRun(run.ID, jobID, agentWorkDir(r.cfg.DataDir, run.ID))
		return
	}

	exec, err := newAgentExecutor(run.ExecutionModel, r.cfg, run.MooncakeAllowShell)
	if err != nil {
		r.emit(elog, ci.EventRunFailed, map[string]any{"error": err.Error()})
		elog.Close()
		_ = storage.FinishTurn(r.db, turn.ID, storage.TurnError)
		log.Error("agent turn executor", "model", run.ExecutionModel, "err", err)
		r.failAgentRun(run.ID, jobID, agentWorkDir(r.cfg.DataDir, run.ID))
		return
	}

	// turn.Seq is the follow-up index (1-based); display number accounts for
	// turn 1 being the issue body. A follow-up resumes the session (claude),
	// so firstTurn is false: the executor omits the system prompt (already in
	// the session) and uses the message as the goal. The MCP config file
	// (written on turn 1) persists in the workspace, so resume sees the same servers.
	mcpPath := "/work/" + agentMCPConfigName
	in := turnInput{
		sessionID: agentSessionID(run.ID),
		message:   turn.Body,
		mcpPath:   mcpPath,
		resume:    true,
	}
	turnCtx, h := r.registerAgentTurn(parent, run.ID)
	status, execErr := r.runAgentTurn(turnCtx, stream, elog, exec, turn.Seq+1, in)
	r.unregisterAgentTurn(run.ID, h)
	elog.Close()

	if h.canceled.Load() {
		// Operator force-stopped this turn (#146): CancelAgentRun owns the
		// terminal state + teardown. Just mark the turn errored and stop.
		log.Info("agent turn canceled by operator", "turn", turn.Seq+1)
		_ = storage.FinishTurn(r.db, turn.ID, storage.TurnError)
		return
	}
	if execErr != nil {
		log.Error("agent turn exec", "err", execErr)
		_ = storage.FinishTurn(r.db, turn.ID, storage.TurnError)
		r.commentAgentFailure(run, fmt.Sprintf("turn %d failed to run", turn.Seq+1))
		r.failAgentRun(run.ID, jobID, agentWorkDir(r.cfg.DataDir, run.ID))
		return
	}
	// The follow-up turn ran to completion — success, a soft stop, or a genuine
	// step failure. Mark the turn done and re-park awaiting_input, keeping the
	// container alive so the operator can Continue again. A non-fatal stop must
	// not end the session; only operator Stop or infra death (execErr, above)
	// does (#178). The turn's verdict rides its turn.completed event + stderr
	// replay (#117), so the transcript still shows what happened.
	if err := storage.FinishTurn(r.db, turn.ID, storage.TurnDone); err != nil {
		log.Error("agent finish turn", "err", err)
	}
	if err := storage.MarkRunAwaitingInput(r.db, run.ID); err != nil {
		// Failed re-park leaves the run 'running' with a live container the idle
		// reaper won't reclaim — tear it down instead of stranding it.
		log.Error("agent re-park awaiting_input", "err", err)
		r.failAgentRun(run.ID, jobID, agentWorkDir(r.cfg.DataDir, run.ID))
		return
	}
	log.Info("agent turn done; awaiting input", "status", status)
}

// runAgentTurn composes and runs one turn under a per-turn deadline via the
// run's executor, streaming each translated output line onto the event log as
// an agent event and bracketing the turn with turn.started/completed. It
// returns the turn status and any executor error (couldn't run the CLI /
// cancelled), but does not itself finalize the run or job — the caller decides
// whether to park or fail. The executor decides what runs (claude vs mooncake
// agent) and how to translate its output.
func (r *ciRunner) runAgentTurn(parent context.Context, stream streamingSession, elog *ci.EventLog, exec agentExecutor, turnNum int, in turnInput) (status string, execErr error) {
	ctx := parent
	if r.cfg.AgentTurnTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(parent, r.cfg.AgentTurnTimeout)
		defer cancel()
	}

	argv := exec.Argv(in)
	r.emit(elog, ci.EventAgentTurnStarted, map[string]any{"turn": turnNum, "prompt": in.goal(), "model": exec.Model()})

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

// registerAgentTurn derives a cancelable context for one turn and records a
// handle so CancelAgentRun can interrupt it (#146). Returns the context to run
// the turn under and the handle to check/unregister afterwards.
func (r *ciRunner) registerAgentTurn(parent context.Context, runID int64) (context.Context, *agentTurnHandle) {
	ctx, cancel := context.WithCancel(parent)
	h := &agentTurnHandle{cancel: cancel}
	r.agentTurns.Store(runID, h)
	return ctx, h
}

// unregisterAgentTurn drops the turn handle and releases its context. Safe to
// call once per registerAgentTurn; a concurrent CancelAgentRun that already
// loaded the handle still cancels correctly.
func (r *ciRunner) unregisterAgentTurn(runID int64, h *agentTurnHandle) {
	r.agentTurns.Delete(runID)
	h.cancel()
}

// CancelAgentRun force-stops an agent run (#146): it CAS-cancels the run in the
// DB from any non-terminal state, interrupts an in-flight turn's blocking
// ExecStream if one is running (so the turn goroutine unwinds and, seeing the
// canceled flag, skips its own finalize), and tears down the container +
// workspace + token. Discard semantics: /work is dropped and no branch is
// materialized — Finish stays the keep-the-work path. Returns true if the run
// was non-terminal and is now canceled, false if it was already terminal.
func (r *ciRunner) CancelAgentRun(runID int64) bool {
	// CAS to canceled first so it's authoritative: a turn goroutine racing to
	// park (MarkRunAwaitingInput, also a CAS) then no-ops, and vice versa.
	if err := storage.CancelAgentRun(r.db, runID); err != nil {
		return false // already terminal (ErrNotFound) or a write error
	}
	// Unblock an in-flight turn, if any, and flag it so its goroutine doesn't
	// re-finalize over the RunCanceled we just wrote.
	if v, ok := r.agentTurns.Load(runID); ok {
		h, _ := v.(*agentTurnHandle) // map only ever holds *agentTurnHandle
		h.canceled.Store(true)
		h.cancel()
	}
	jobID, _ := r.agentJobID(runID)
	if jobID != 0 {
		r.finishJob(jobID, storage.JobInterrupted, nil)
	}
	r.tearDownAgent(runID, jobID, agentWorkDir(r.cfg.DataDir, runID))
	return true
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
		_ = os.RemoveAll(workDir)
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
