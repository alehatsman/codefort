package main

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/alehatsman/codefort/internal/ci"
	"github.com/alehatsman/codefort/internal/config"
	"github.com/alehatsman/codefort/internal/storage"
)

// provisionEvent is one line of `provision apply <plan> --json` output —
// either a step event or the trailing summary line (spec §9.3). rc/stdout/
// stderr/diff/reason/message are present only when provision has them for
// that line: rc/stdout/stderr on a step that ran a command (shell/cmd/
// assert), whatever its status; diff only when there is one; reason only on
// a skip; message (+ rc/stderr again) only on a failure. A typed action or a
// skipped step carries none of the command fields — see docs/
// ops-provisioning.md's CI-runner section, item 3.
type provisionEvent struct {
	Event      string `json:"event"` // "step" | "summary"
	Index      int    `json:"index,omitempty"`
	Line       int    `json:"line,omitempty"`
	File       string `json:"file,omitempty"`
	Name       string `json:"name,omitempty"`
	Status     string `json:"status,omitempty"` // ok | changed | unknown | skipped | failed | would_change | would_run | would_run_unprobed
	RC         *int   `json:"rc,omitempty"`
	Stdout     string `json:"stdout,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
	Diff       string `json:"diff,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Message    string `json:"message,omitempty"`
	DurationMS int    `json:"duration_ms,omitempty"`

	// Summary-only fields (Event == "summary").
	Plan             string `json:"plan,omitempty"`
	Total            int    `json:"total,omitempty"`
	OK               int    `json:"ok,omitempty"`
	Changed          int    `json:"changed,omitempty"`
	Skipped          int    `json:"skipped,omitempty"`
	Unknown          int    `json:"unknown,omitempty"`
	Failed           int    `json:"failed,omitempty"`
	WouldChange      int    `json:"would_change,omitempty"`
	WouldRun         int    `json:"would_run,omitempty"`
	WouldRunUnprobed int    `json:"would_run_unprobed,omitempty"`
	Interrupted      bool   `json:"interrupted,omitempty"`
}

// The runner's external boundaries, injected so the orchestration is testable
// without the real git / provision / docker binaries.
type (
	// planExecutor runs one job's provision plan (already written at planFile,
	// relative to workDir) in workDir, streaming decoded NDJSON events to
	// onEvent as they arrive and returning the trailing summary once the
	// process exits. It backs the host session and is the unit tests'
	// injection point.
	planExecutor func(ctx context.Context, workDir, planFile string, onEvent func(provisionEvent)) (provisionEvent, error)
	// checkoutFunc materializes the repo tree at commitSHA into workDir.
	checkoutFunc func(ctx context.Context, bareRepo, commitSHA, workDir string) error
	// pipelineReader reads codefort.yml at commitSHA; ok=false means absent.
	pipelineReader func(ctx context.Context, bareRepo, commitSHA string) (raw []byte, ok bool, err error)
)

// jobSession executes one job's provision plan in some environment and is
// closed when the job finishes. It is the runner's isolation seam: runJob
// emits the same event stream regardless of whether the session runs the
// plan on the host or in a per-job container.
type jobSession interface {
	// ExecPlan runs the plan at planFile — a path resolved against the
	// session's own working directory (the host workDir for hostSession,
	// "/work/<planFile>" inside the container for dockerSession) — streaming
	// each decoded NDJSON step event to onEvent as it arrives and returning
	// the trailing summary event once the process exits.
	ExecPlan(ctx context.Context, planFile string, onEvent func(provisionEvent)) (provisionEvent, error)
	Close() error
}

// streamingSession is the agent counterpart to jobSession.Exec: it runs a
// command in the session's environment and streams its stdout to onLine one
// line at a time (each line keeping its trailing newline), returning the
// process exit code. onStderr receives the command's stderr lines (after stdout
// is fully drained), so a turn whose tool logs diagnostics only to stderr — like
// mooncake — is never rendered blank on failure (#117); pass nil to ignore it.
// It's a separate capability — CI's per-step JSON contract is buffered, while a
// live agent transcript must surface each claude stream-json line as it's
// written. Both concrete sessions implement it; the agent executor type-asserts
// for it.
type streamingSession interface {
	ExecStream(ctx context.Context, argv []string, onLine, onStderr func(line []byte)) (exitCode int, err error)
}

// streamExecutor backs hostSession.ExecStream — the injection point that lets
// agent executor tests feed canned claude stream-json without a real binary.
type streamExecutor func(ctx context.Context, workDir string, argv []string, onLine, onStderr func(line []byte)) (int, error)

// sessionFactory opens a jobSession for one job. name is a stable
// docker-safe container name; workDir is the checked-out (bind-mountable)
// workspace; image is the resolved container image (ignored by the host
// session).
type sessionFactory func(ctx context.Context, name, workDir, image string, extraVols []string) (jobSession, error)

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

	// Agent turn loop: newAgentSession opens the turn-1 container with the
	// per-run env (creds) injected and host reachability; attachSession binds to
	// that already-running container (no docker run) so a follow-up turn can
	// resume the session; teardownContainer removes a finalized agent container.
	// All injected so the turn-loop tests run without docker.
	newAgentSession   func(ctx context.Context, name, workDir, image string, env []string) (jobSession, error)
	attachSession     func(ctx context.Context, name string) (jobSession, error)
	teardownContainer func(name string)

	// agentTurns tracks in-flight agent turns so an operator force-stop (#146)
	// can interrupt the otherwise-blocking ExecStream. Keyed by run ID, the
	// value is *agentTurnHandle; registered for the duration of a turn and
	// deleted when it returns.
	agentTurns sync.Map

	// runCancels tracks in-flight CI runs so an operator force-stop (#296) can
	// interrupt executeRun. Keyed by run ID, value *agentTurnHandle (a generic
	// cancel handle): cancel unwinds the run's job loop, and the canceled flag
	// tells executeRun to finalize as RunCanceled (operator) rather than
	// RunInterrupted (shutdown). Registered for the duration of executeRun.
	runCancels sync.Map
}

// agentTurnHandle is a generic cancel handle: cancel unblocks the work (an
// agent turn's ExecStream, or a CI run's job loop), and the canceled flag tells
// the owning goroutine that the operator (not an infra error) ended it — so an
// agent turn skips its own finalize (CancelAgentRun owns teardown) and a CI run
// finalizes terminal as canceled rather than interrupted.
type agentTurnHandle struct {
	cancel   context.CancelFunc
	canceled atomic.Bool
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
		// Legacy path: steps run on the host as the codefortd user.
		r.newSession = func(_ context.Context, _, workDir, _ string, _ []string) (jobSession, error) {
			return &hostSession{workDir: workDir, exec: runProvisionPlanHost, stream: runClaudeStreamHost}, nil
		}
		r.newAgentSession = func(_ context.Context, _, workDir, _ string, _ []string) (jobSession, error) {
			return &hostSession{workDir: workDir, exec: runProvisionPlanHost, stream: runClaudeStreamHost}, nil
		}
		r.attachSession = func(_ context.Context, _ string) (jobSession, error) {
			return nil, errors.New("agent turn resume requires docker isolation")
		}
		r.teardownContainer = func(string) {}
	} else {
		// Default: one throwaway container per job, steps run via docker exec.
		r.newSession = func(ctx context.Context, name, workDir, image string, extraVols []string) (jobSession, error) {
			return openDockerSession(ctx, logger, name, workDir, image, extraVols)
		}
		// The agent's turn-1 container carries the per-run env + host reachability.
		r.newAgentSession = func(ctx context.Context, name, workDir, image string, env []string) (jobSession, error) {
			return openAgentDockerSession(ctx, logger, name, workDir, image, env)
		}
		// Resume binds to the still-running agent container by name.
		r.attachSession = func(_ context.Context, name string) (jobSession, error) {
			return &dockerSession{name: name, logger: logger}, nil
		}
		r.teardownContainer = func(name string) { removeContainer(logger, name) }
	}
	return r
}

// runCIRunner launches the in-process CI runner beside the reapers. It claims
// and dispatches runs from one shared budget (MaxConcurrency, default NumCPU)
// that CI and agent runs share, with AgentReserved slots kept for agents. Runs
// until ctx is cancelled.
func runCIRunner(ctx context.Context, r *ciRunner) {
	r.run(ctx)
}

// defaultCIPollInterval matches config's documented default; minCIPollInterval
// is the floor below which polling is pure waste — a queued run is noticed
// within a poll either way, and nobody is waiting on 100ms of latency here.
const (
	defaultCIPollInterval = 5 * time.Second
	minCIPollInterval     = 100 * time.Millisecond
)

func (r *ciRunner) run(ctx context.Context) {
	// <= 0 means "unset" and falls back to the default. A tiny-but-positive
	// value is the one that actually hurts: `CODEFORT_CI_POLL_INTERVAL=1ms`
	// parses fine and spins this loop a thousand times a second against SQLite
	// for no benefit, so floor it at something a human could plausibly mean.
	interval := r.cfg.CIPollInterval
	switch {
	case interval <= 0:
		interval = defaultCIPollInterval
	case interval < minCIPollInterval:
		r.logger.Warn("CODEFORT_CI_POLL_INTERVAL is below the floor; using the floor",
			"configured", interval, "floor", minCIPollInterval)
		interval = minCIPollInterval
	}
	maxConc := max(r.cfg.MaxConcurrency, 1)
	r.logger.Info("ci runner started", "poll", interval, "run_timeout", r.cfg.CIRunTimeout, "isolation", r.cfg.CIIsolation, "max_concurrency", maxConc, "agent_reserved", r.cfg.AgentReserved)

	// A restart can strand runs mid-flight: their status writes never committed,
	// so they sit 'running' with no goroutine driving them. Nothing can be
	// legitimately in flight before we take our first run, so finalize any such
	// orphans now rather than leaving the UI with a job stuck forever.
	if n, err := storage.ReconcileOrphanRuns(r.db); err != nil {
		r.logger.Error("ci reconcile orphan runs", "err", err)
	} else if n > 0 {
		r.logger.Info("ci reconciled orphaned runs", "count", n)
	}

	// Ephemeral agent-run tokens are revoked on finalize; a crash can strand
	// some valid. Their runs were just reconciled, so none should still be
	// live — revoke any leftovers before taking new work.
	if n, err := storage.RevokeAgentRunTokens(r.db); err != nil {
		r.logger.Error("agent revoke leftover tokens", "err", err)
	} else if n > 0 {
		r.logger.Info("agent revoked leftover run tokens", "count", n)
	}

	// A crashed runner can leave job containers behind; reap them before
	// taking new work so they don't accumulate.
	if r.cfg.CIIsolation == "docker" {
		sweepOrphanContainers(ctx, r.logger)
	}

	// CI and agent-family runs draw from one shared budget (#293): up to
	// MaxConcurrency runs in flight, with AgentReserved slots only agents may
	// take so a CI backlog can't lock out a spawn. The drain is non-blocking
	// (try-acquire), so neither kind ever blocks the loop behind the other (the
	// #268 starvation). wg tracks in-flight runs so shutdown drains them: run()
	// returns only once every dispatched executeRun has finalized its run
	// against the still-open DB (the restart-drain contract — see main.go).
	// An AgentReserved at or above the total silently leaves CI zero slots —
	// the budget clamps it, and CI then simply never runs, which presents as a
	// mysteriously dead pipeline rather than as a misconfiguration. Say so.
	if r.cfg.AgentReserved >= maxConc {
		r.logger.Warn("agent reservation leaves no CI slots; CI runs will never start",
			"agent_reserved", r.cfg.AgentReserved, "max_concurrency", maxConc)
	}
	budget := newWorkBudget(maxConc, r.cfg.AgentReserved)
	var wg sync.WaitGroup
	defer wg.Wait()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		// Dispatch every claimable run that fits the budget before sleeping.
		r.drainKind(ctx, &wg, budget.tryAcquireCI, budget.releaseCI, storage.RunKindCI, r.runLease())
		// The agent family (agent, spec-verify, …) draws the agent budget side;
		// drain each kind in AgentRunKinds so a new agent kind is picked up
		// without editing this loop (#270).
		for _, k := range storage.AgentRunKinds {
			r.drainKind(ctx, &wg, budget.tryAcquireAgent, budget.releaseAgent, k, r.agentLease())
		}
		// Reap lifetime-expired parked agent sessions, then dispatch queued
		// follow-up turns + accepted handoffs. Both are agent-family work: a
		// parked run holds no slot; dispatching briefly takes one.
		r.reapExpiredAgents(ctx)
		r.drainTurns(ctx, &wg, budget.tryAcquireAgent, budget.releaseAgent)
		r.drainFinishing(ctx, &wg, budget.tryAcquireAgent, budget.releaseAgent)
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

// drainTurns claims and dispatches every currently-dispatchable follow-up turn
// (a parked run's next pending message) that fits the agent budget, returning
// when nothing more is claimable, the budget is full, or the context is
// cancelled. Mirrors drainKind.
func (r *ciRunner) drainTurns(ctx context.Context, wg *sync.WaitGroup, acquire func() bool, release func()) {
	for {
		if ctx.Err() != nil {
			return
		}
		if !acquire() {
			return // budget full — retry next poll
		}
		turn, run, err := storage.ClaimNextTurn(r.db, r.turnLease())
		if err != nil {
			release()
			if !errors.Is(err, storage.ErrNoTurnPending) {
				r.logger.Error("agent claim turn", "err", err)
			}
			return
		}
		wg.Add(1)
		go func(turn storage.AgentTurn, run storage.CIRun) {
			defer wg.Done()
			defer release()
			r.dispatchTurn(ctx, turn, run)
		}(turn, run)
	}
}

// drainFinishing claims and dispatches every agent run the human has accepted
// (state finishing) that fits the agent budget, performing handoff for each.
// Mirrors drainKind/drainTurns.
func (r *ciRunner) drainFinishing(ctx context.Context, wg *sync.WaitGroup, acquire func() bool, release func()) {
	for {
		if ctx.Err() != nil {
			return
		}
		if !acquire() {
			return // budget full — retry next poll
		}
		run, err := storage.ClaimNextFinishingRun(r.db)
		if err != nil {
			release()
			if !errors.Is(err, storage.ErrNoRunQueued) {
				r.logger.Error("agent claim finishing", "err", err)
			}
			return
		}
		wg.Add(1)
		go func(run storage.CIRun) {
			defer wg.Done()
			defer release()
			r.finishAgentRun(ctx, run)
		}(run)
	}
}

// drainKind claims and dispatches every currently-claimable run of one kind
// that fits the shared budget, returning when nothing more is claimable, the
// budget is full, or the context is cancelled. acquire is non-blocking — a full
// budget returns false so this kind never blocks the loop behind another
// (#268); the slot is taken *before* claiming so a claimed run always has a slot
// to run in (its lease never ticks while it waits). Each dispatched run is
// tracked on wg for the shutdown drain.
func (r *ciRunner) drainKind(ctx context.Context, wg *sync.WaitGroup, acquire func() bool, release func(), kind storage.RunKind, lease time.Duration) {
	for {
		// Once shutdown starts, stop claiming new work; in-flight runs drain
		// via the caller's wg.Wait. Guarding here also keeps us from claiming a
		// queued run only to immediately error it.
		if ctx.Err() != nil {
			return
		}
		if !acquire() {
			return // budget full — retry next poll
		}
		run, err := storage.ClaimNextRunOfKind(r.db, kind, lease)
		if err != nil {
			release() // release the unused slot
			if !errors.Is(err, storage.ErrNoRunQueued) {
				r.logger.Error("ci claim", "kind", kind, "err", err)
			}
			return
		}
		wg.Add(1)
		go func(run storage.CIRun) {
			defer wg.Done()
			defer release()
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

// turnLease is the claim lease for follow-up turns, derived from the per-turn
// timeout: long enough that a live dispatch is never stolen, short enough that
// a crashed dispatch's 'running' turn eventually re-dispatches.
func (r *ciRunner) turnLease() time.Duration {
	if r.cfg.AgentTurnTimeout <= 0 {
		return 0
	}
	return r.cfg.AgentTurnTimeout + 2*time.Minute
}

// executeRun runs one claimed run end-to-end: gate, checkout, per-job step
// execution emitting the event stream, status mirroring, and workspace
// cleanup.
func (r *ciRunner) executeRun(parent context.Context, run storage.CIRun) {
	// An agent run reuses this same claim/lease/drain spine but executes a
	// containerized Claude session against an issue instead of a translated
	// codefort.yml. Branch here so everything upstream (claiming, concurrency,
	// reconcile, retention) stays shared.
	if run.Kind.IsAgent() {
		r.executeAgentRun(parent, run)
		return
	}

	log := r.logger.With("run_id", run.ID, "run", run.Number, "sha", short(run.CommitSHA))

	owner, name, err := storage.RepoIdent(r.db, run.RepoID)
	if err != nil {
		log.Error("ci resolve repo", "err", err)
		r.finish(run, storage.RunError)
		return
	}
	log = log.With("repo", owner+"/"+name)

	// Gate: repo must be CI-enabled AND codefort.yml must exist at the commit.
	enabled, err := storage.RepoCIEnabled(r.db, run.RepoID)
	if err != nil {
		log.Error("ci enabled check", "err", err)
		r.finish(run, storage.RunError)
		return
	}
	bareRepo := filepath.Join(r.cfg.ReposDir, owner, name+".git")
	raw, ok, err := r.readPipeline(parent, bareRepo, run.CommitSHA)
	if err != nil {
		log.Error("ci read codefort.yml", "err", err)
		r.finish(run, storage.RunError)
		return
	}
	if !enabled || !ok {
		log.Info("ci run skipped (gated)", "enabled", enabled, "has_pipeline", ok)
		r.finish(run, storage.RunCanceled)
		return
	}

	pipeline, err := ci.Parse(raw)
	if err != nil {
		log.Error("ci parse codefort.yml", "err", err)
		r.finish(run, storage.RunError)
		return
	}
	order, err := pipeline.TopoOrder()
	if err != nil {
		log.Error("ci topo order", "err", err)
		r.finish(run, storage.RunError)
		return
	}

	// Fresh workspace; always cleaned up.
	workDir := filepath.Join(r.cfg.DataDir, "ci", "work", strconv.FormatInt(run.ID, 10))
	if err := os.RemoveAll(workDir); err != nil {
		log.Error("ci workspace", "err", err)
		r.finish(run, storage.RunError)
		return
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		log.Error("ci workspace", "err", err)
		r.finish(run, storage.RunError)
		return
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	// Always derive a cancelable ctx and register a handle so an operator
	// force-stop (#296) can interrupt the run; layer the run timeout on top when
	// configured. The handle's canceled flag distinguishes that operator stop
	// from a shutdown in the finalizer below.
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	if r.cfg.CIRunTimeout > 0 {
		var timeoutCancel context.CancelFunc
		ctx, timeoutCancel = context.WithTimeout(ctx, r.cfg.CIRunTimeout)
		defer timeoutCancel()
	}
	handle := &agentTurnHandle{cancel: cancel}
	r.runCancels.Store(run.ID, handle)
	defer r.runCancels.Delete(run.ID)

	if err := r.checkout(ctx, bareRepo, run.CommitSHA, workDir); err != nil {
		log.Error("ci checkout", "err", err)
		r.finish(run, storage.RunError)
		return
	}

	// Create job rows up front, in topo order.
	jobIDs := make(map[string]int64, len(order))
	for _, jn := range order {
		job, err := storage.CreateJob(r.db, run.ID, jn, pipeline.Jobs[jn].Needs)
		if err != nil {
			log.Error("ci create job", "job", jn, "err", err)
			r.finish(run, storage.RunError)
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
			// No job is ready and none was skippable this pass. A wave is fully
			// awaited below, so "no ready, no skip" can only be a malformed DAG
			// that slipped past TopoOrder's validation. Finalize the stuck jobs
			// as errored rather than leaving them queued forever (the post-loop
			// finalizer only covers the ctx-cancelled case), then stop.
			log.Error("ci scheduling stuck: no runnable job", "pending", len(pending))
			for jn := range pending {
				r.finishJob(jobIDs[jn], storage.JobError, nil)
				status[jn] = storage.JobError
				delete(pending, jn)
				anyNotSuccess = true
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
	// A run-timeout / shutdown can leave jobs that never started still pending
	// (queued in the DB). Finalize them so a terminal run has no dangling queued
	// jobs: interrupted under a shutdown (Canceled), errored under a run-timeout.
	if ctx.Err() != nil {
		leftover := storage.JobError
		if errors.Is(ctx.Err(), context.Canceled) {
			leftover = storage.JobInterrupted
		}
		for jn := range pending {
			r.finishJob(jobIDs[jn], leftover, nil)
		}
	}

	// A shutdown (context.Canceled) cut the run short — operator-induced, so
	// terminal-but-neutral (interrupted), not a red error. A run-timeout
	// (DeadlineExceeded) is a genuine infrastructure failure (error).
	final := storage.RunSuccess
	switch {
	case handle.canceled.Load():
		// Operator force-stop (#296): terminal-and-intentional (canceled), distinct
		// from a shutdown's neutral interrupted. Checked first since it also trips
		// the context.Canceled case below.
		final = storage.RunCanceled
	case errors.Is(ctx.Err(), context.Canceled):
		final = storage.RunInterrupted
	case ctx.Err() != nil:
		final = storage.RunError
	case anyNotSuccess:
		final = storage.RunFailed
	}
	r.finish(run, final)
	log.Info("ci run finished", "status", final)
}

// runJob executes one job's steps via provision, emitting the event stream
// into the job's events.jsonl, and returns its terminal status.
func (r *ciRunner) runJob(ctx context.Context, owner, repo string, runNum int, jobName string, jobID int64, job ci.Job, workDir string) storage.JobStatus {
	log := r.logger.With("run", runNum, "job", jobName)

	meta, err := ci.JobStepMeta(job)
	if err != nil {
		log.Error("ci translate steps", "err", err)
		r.finishJob(jobID, storage.JobError, nil)
		return storage.JobError
	}
	planYAML, err := ci.TranslateJobPlan(job)
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
	r.emit(elog, ci.EventRunStarted, map[string]any{"total_steps": len(meta)})
	r.emit(elog, ci.EventPlanLoaded, map[string]any{"total_steps": len(meta)})

	// The plan lives in the job's own workspace so it's reachable inside a
	// per-job container at /work/<name>, the same way the checked-out repo
	// already is (openDockerSession bind-mounts workDir at /work).
	planFile := jobName + ".plan.yml"
	if err := os.WriteFile(filepath.Join(workDir, planFile), planYAML, 0o644); err != nil {
		log.Error("ci write plan", "err", err)
		r.finishJob(jobID, storage.JobError, nil)
		return storage.JobError
	}

	// Open the job's execution environment (a per-job container under docker
	// isolation, or the host otherwise). A failure here — e.g. the image is
	// missing or docker is down — fails the job loudly rather than silently
	// falling back to the host.
	image := job.Image
	if image == "" {
		image = r.cfg.CIDefaultImage
	}
	var extraVols []string
	if job.DockerSocket {
		extraVols = append(extraVols, "/var/run/docker.sock:/var/run/docker.sock")
	}
	sess, err := r.newSession(ctx, containerName(jobID, jobName), r.hostPath(workDir), image, extraVols)
	if err != nil {
		// A shutdown mid-open cancels the session's context — interrupted, not a
		// failure to provision the environment.
		if errors.Is(ctx.Err(), context.Canceled) {
			r.emit(elog, ci.EventStepStderr, map[string]any{
				"step_id": "session", "stream": "stderr", "line": "session interrupted: runner shutting down", "line_number": 1,
			})
			r.finishJob(jobID, storage.JobInterrupted, nil)
			log.Info("ci session interrupted by shutdown", "image", image)
			return storage.JobInterrupted
		}
		r.emit(elog, ci.EventStepStderr, map[string]any{
			"step_id": "session", "stream": "stderr", "line": err.Error(), "line_number": 1,
		})
		r.emit(elog, ci.EventRunFailed, map[string]any{"error": err.Error()})
		r.finishJob(jobID, storage.JobError, nil)
		log.Error("ci open session", "image", image, "err", err)
		return storage.JobError
	}
	defer sess.Close()

	// provision's --json stream reports a step only once it has finished —
	// there is no separate "step started" line (docs/ops-provisioning.md's
	// CI-runner section). `apply` runs a job's steps strictly sequentially, so
	// step N's event arriving means step N+1 is starting; pre-declare step
	// 1's start now (already known from the plan just written), then chain
	// each later start off the previous step's arrival below. next holds the
	// 1-based id of the step currently in flight (started, not yet reported).
	next := 0
	startStep := func(i int) {
		if i >= len(meta) {
			return
		}
		stepID := fmt.Sprintf("step-%04d", i+1)
		r.emit(elog, ci.EventStepStarted, map[string]any{
			"step_id": stepID, "action": meta[i].Action, "name": meta[i].Label, "global_step": i + 1,
		})
		next = i + 1
	}
	startStep(0)

	var failedStep *provisionEvent
	_, execErr := sess.ExecPlan(ctx, planFile, func(ev provisionEvent) {
		i := ev.Index - 1 // provision's index is 1-based, matching meta's order
		if i < 0 || i >= len(meta) {
			// Defensive: an index outside the plan we generated would be a
			// provision/translator mismatch, not a normal outcome — fall back
			// to the step we're expecting rather than mis-indexing meta.
			i = next - 1
		}
		stepID := fmt.Sprintf("step-%04d", i+1)

		emitLines(r, elog, stepID, ci.EventStepStdout, "stdout", ev.Stdout)
		emitLines(r, elog, stepID, ci.EventStepStderr, "stderr", ev.Stderr)

		failed := ev.Status == "failed"
		result := map[string]any{"status": ev.Status, "failed": failed}
		if ev.RC != nil {
			result["rc"] = *ev.RC
		}
		if ev.Stdout != "" {
			result["stdout"] = ev.Stdout
		}
		if ev.Stderr != "" {
			result["stderr"] = ev.Stderr
		}
		r.emit(elog, ci.EventStepCompleted, map[string]any{
			"step_id": stepID, "duration_ms": ev.DurationMS,
			"changed": ev.Status == "changed",
			"result":  result,
		})

		if failed {
			e := ev
			failedStep = &e
		}
		// The step just reported as done; its successor (if any) is what
		// `apply` runs next — provision stops at the first failure, so no
		// further events arrive once failedStep is set.
		startStep(i + 1)
	})

	if execErr != nil {
		stepID := fmt.Sprintf("step-%04d", next)
		// A graceful shutdown cancels the plan's context — parent cancellation
		// surfaces as context.Canceled, distinct from a run-timeout's
		// DeadlineExceeded. That's operator-induced (a deploy/restart), not a
		// gate failure, so finalize the job interrupted — neutral, not error.
		if errors.Is(ctx.Err(), context.Canceled) {
			r.emit(elog, ci.EventStepStderr, map[string]any{
				"step_id": stepID, "stream": "stderr", "line": "step interrupted: runner shutting down", "line_number": 1,
			})
			r.emit(elog, ci.EventStepCompleted, map[string]any{
				"step_id": stepID, "result": map[string]any{"rc": -1, "failed": true, "status": "interrupted"},
			})
			r.finishJob(jobID, storage.JobInterrupted, nil)
			log.Info("ci step interrupted by shutdown", "step", stepID)
			return storage.JobInterrupted
		}
		// Couldn't run the plan (provision missing, or run-timeout).
		r.emit(elog, ci.EventStepStderr, map[string]any{
			"step_id": stepID, "stream": "stderr", "line": execErr.Error(), "line_number": 1,
		})
		r.emit(elog, ci.EventStepCompleted, map[string]any{
			"step_id": stepID, "result": map[string]any{"rc": -1, "failed": true, "status": "error"},
		})
		r.emit(elog, ci.EventRunFailed, map[string]any{"step_id": stepID, "error": execErr.Error()})
		r.finishJob(jobID, storage.JobError, nil)
		log.Error("ci plan exec", "step", stepID, "err", execErr)
		return storage.JobError
	}

	if failedStep != nil {
		rc := 1
		if failedStep.RC != nil {
			rc = *failedStep.RC
		}
		stepID := fmt.Sprintf("step-%04d", failedStep.Index)
		r.emit(elog, ci.EventRunFailed, map[string]any{"step_id": stepID, "rc": rc})
		r.finishJob(jobID, storage.JobFailed, &rc)
		log.Info("ci job failed", "step", stepID, "rc", rc)
		return storage.JobFailed
	}

	r.emit(elog, ci.EventRunCompleted, map[string]any{"total_steps": len(meta)})
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

func (r *ciRunner) finish(run storage.CIRun, status storage.RunStatus) {
	if err := storage.FinishRun(r.db, run.ID, status); err != nil {
		r.logger.Error("ci finish run", "run_id", run.ID, "status", status, "err", err)
	}
	// Mirror the terminal status onto the outbound fleet feed (#73), the
	// counterpart to the server's ci.run.queued. Agent runs reuse this spine but
	// aren't CI, so they get their own agent.run.finished lifecycle event (#407)
	// rather than the misleading ci.run.finished. Best-effort: a feed write must
	// not mask the run's real outcome.
	if run.Kind.IsAgent() {
		r.emitAgentEvent(run, "agent.run.finished", map[string]any{"status": string(status)})
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"number": run.Number,
		"status": string(status),
		"ref":    run.Ref,
		"sha":    run.CommitSHA,
	})
	if _, err := storage.AppendEvent(r.db, "ci.run.finished", run.RepoID, run.Trigger, string(payload)); err != nil {
		r.logger.Error("ci finished event", "run_id", run.ID, "err", err)
	}
}

// emitAgentEvent records an agent-run lifecycle event on the outbound fleet
// feed (#407): agent.run.started / awaiting_input / finished. It always carries
// the run number and, when linked, the issue it's working, so the activity feed
// and triage panel can render and link it. Best-effort — a feed write must
// never mask the run's real progress.
func (r *ciRunner) emitAgentEvent(run storage.CIRun, eventType string, extra map[string]any) {
	data := map[string]any{"number": run.Number}
	if run.IssueNumber != nil {
		data["issue_number"] = *run.IssueNumber
	}
	for k, v := range extra {
		data[k] = v
	}
	payload, _ := json.Marshal(data)
	if _, err := storage.AppendEvent(r.db, eventType, run.RepoID, run.Trigger, string(payload)); err != nil {
		r.logger.Error("agent feed event", "type", eventType, "run_id", run.ID, "err", err)
	}
}

func (r *ciRunner) finishJob(jobID int64, status storage.JobStatus, exitCode *int) {
	if err := storage.FinishJob(r.db, jobID, status, exitCode); err != nil {
		r.logger.Error("ci finish job", "job_id", jobID, "status", status, "err", err)
	}
}

// hostSession runs a job's plan on the host via the injected planExecutor
// (the legacy, non-isolated path; also the unit tests' seam). It owns no
// resources, so Close is a no-op.
type hostSession struct {
	workDir string
	exec    planExecutor
	stream  streamExecutor
}

func (h *hostSession) ExecPlan(ctx context.Context, planFile string, onEvent func(provisionEvent)) (provisionEvent, error) {
	return h.exec(ctx, h.workDir, planFile, onEvent)
}

// ExecStream runs an agent command on the host. nil stream means this session
// wasn't built for agent work (the CI host path) — agent runs require docker.
func (h *hostSession) ExecStream(ctx context.Context, argv []string, onLine, onStderr func(line []byte)) (int, error) {
	if h.stream == nil {
		return -1, errors.New("host session does not support streaming exec")
	}
	return h.stream(ctx, h.workDir, argv, onLine, onStderr)
}

func (h *hostSession) Close() error { return nil }

// dockerSession runs a job's plan inside a single throwaway container, keeping
// repo-authored commands off the host. The container is started detached
// (`sleep infinity`) at Open and torn down at Close; the plan runs as one
// `docker exec provision apply /work/<plan> --json` into it, so the job
// shares the bind-mounted workspace and the streamed NDJSON contract is
// identical to the host path.
type dockerSession struct {
	name   string
	logger *slog.Logger
}

// openDockerSession starts the per-job container. The workspace is bind-mounted
// at /work and the container runs as the codefortd uid:gid so files it writes
// stay owned by codefortd (root-owned files would break workspace cleanup). The
// image must be glibc-based and carry `provision` on PATH (see ci/Dockerfile);
// `mooncake` stays on PATH too as long as the `quality` job's `mooncake task
// ci` shell-out is in scope (#411 explicitly excludes the goq/tq rewrite).
//
// Host reachability (host.docker.internal -> the host gateway, same mapping the
// agent path uses) lets a job reach this codefort — needed by the `smoke` job's
// (translated) http asserts, which hit host.docker.internal:8080 directly.
func openDockerSession(ctx context.Context, logger *slog.Logger, name, workDir, image string, extraVols []string) (jobSession, error) {
	args := []string{
		"run", "-d", "--rm",
		"--name", name,
		"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		"-v", workDir + ":/work",
		"-w", "/work",
		"--add-host", "host.docker.internal:host-gateway",
	}
	for _, v := range extraVols {
		args = append(args, "-v", v)
		// When the Docker socket is mounted, add its owning GID so the
		// container user can access the 0660 socket without knowing the
		// group name inside the image.
		if strings.HasPrefix(v, "/var/run/docker.sock:") {
			if info, serr := os.Stat("/var/run/docker.sock"); serr == nil {
				if st, ok := info.Sys().(*syscall.Stat_t); ok {
					args = append(args, "--group-add", fmt.Sprintf("%d", st.Gid))
				}
			}
		}
	}
	args = append(args, "--entrypoint", "sleep", image, "infinity")
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker run %s: %v (%s)", image, err, strings.TrimSpace(string(out)))
	}
	return &dockerSession{name: name, logger: logger}, nil
}

// openAgentDockerSession starts the agent's container like openDockerSession but
// injects the per-run env (creds, scoped token) and gives it host
// reachability (host.docker.internal -> the host gateway), so the in-container
// claude/cf shim can reach codefortd and the LLM endpoint. The container
// stays alive (sleep infinity) across turns; teardown is
// explicit (it is not removed when a turn's session handle is dropped).
func openAgentDockerSession(ctx context.Context, logger *slog.Logger, name, workDir, image string, env []string) (jobSession, error) {
	args := []string{
		"run", "-d", "--rm",
		"--name", name,
		"--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		"-v", workDir + ":/work",
		"-w", "/work",
		// Reach the host's codefortd / LLM endpoint. WSL2 maps
		// host-gateway to the host, same as Docker Desktop.
		"--add-host", "host.docker.internal:host-gateway",
	}
	// Pass the per-run secrets (Claude/LLM tokens, the ephemeral cf token,
	// LLM bearer) via --env-file rather than `-e KEY=VALUE`: the latter puts every
	// value on the docker-run argv (visible in `ps`/proc) and bakes it into
	// `docker inspect`.Config.Env for the container's whole lifetime. The 0600
	// file is read by docker only during run and removed right after.
	if len(env) > 0 {
		envFile, err := writeAgentEnvFile(env)
		if err != nil {
			return nil, fmt.Errorf("agent env file: %w", err)
		}
		defer func() { _ = os.Remove(envFile) }()
		args = append(args, "--env-file", envFile)
	}
	args = append(args, "--entrypoint", "sleep", image, "infinity")
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker run %s: %v (%s)", image, err, strings.TrimSpace(string(out)))
	}
	return &dockerSession{name: name, logger: logger}, nil
}

// writeAgentEnvFile writes the agent container's env to a private (0600) temp
// file in docker --env-file format (one KEY=VALUE per line), so the per-run
// secrets never appear on the docker-run argv or in `docker inspect`. The
// caller removes it once `docker run` has consumed it. Values are single-line
// (tokens, URLs), which the KEY=VALUE-per-line format requires.
func writeAgentEnvFile(env []string) (string, error) {
	f, err := os.CreateTemp("", "codefort-agent-env-*")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := f.Chmod(0o600); err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	if _, err := f.WriteString(strings.Join(env, "\n") + "\n"); err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func (d *dockerSession) ExecPlan(ctx context.Context, planFile string, onEvent func(provisionEvent)) (provisionEvent, error) {
	cmd := exec.CommandContext(ctx, "docker", "exec", d.name, "provision", "apply", "/work/"+planFile, "--json")
	return runProvisionPlan(ctx, cmd, onEvent)
}

// ExecStream runs an agent command in the container and streams its stdout
// line-by-line — the live claude transcript path.
func (d *dockerSession) ExecStream(ctx context.Context, argv []string, onLine, onStderr func(line []byte)) (int, error) {
	cmd := exec.CommandContext(ctx, "docker", append([]string{"exec", d.name}, argv...)...)
	return streamCommand(ctx, cmd, onLine, onStderr)
}

// streamCommand starts cmd and forwards each stdout line (newline kept) to
// onLine as it arrives, returning the process exit code once it exits. A
// ReadBytes loop (not bufio.Scanner) avoids the 64 KB line cap, since a single
// claude stream-json object — a big tool result — can exceed it. A non-zero
// exit is returned as the code with a nil error (the caller maps exit/result
// onto run status); only a failure to start or run the process, or a cancelled
// context, is an executor error.
//
// stderr is drained concurrently (so a full pipe can't deadlock the child) and,
// once stdout is exhausted, replayed line-by-line to onStderr — after every
// onLine call, so both callbacks run on this one goroutine and the caller never
// has to synchronize its event log. onStderr may be nil.
func streamCommand(ctx context.Context, cmd *exec.Cmd, onLine, onStderr func(line []byte)) (int, error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return -1, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return -1, err
	}
	if err := cmd.Start(); err != nil {
		return -1, err
	}
	// Accumulate stderr off-goroutine; we replay it after stdout below.
	var stderr bytes.Buffer
	var stderrDone sync.WaitGroup
	stderrDone.Add(1)
	go func() {
		defer stderrDone.Done()
		_, _ = io.Copy(&stderr, stderrPipe)
	}()
	r := bufio.NewReader(stdout)
	for {
		line, rerr := r.ReadBytes('\n')
		if len(line) > 0 {
			onLine(line)
		}
		if rerr != nil {
			break // EOF (process closing stdout) or read error; Wait reports the real outcome
		}
	}
	stderrDone.Wait()
	// Surface the tool's stderr (where mooncake and most CLIs write diagnostics)
	// after the stdout transcript. SplitAfter keeps the buffer intact for the
	// exec-error message below.
	if onStderr != nil && stderr.Len() > 0 {
		for _, line := range bytes.SplitAfter(stderr.Bytes(), []byte{'\n'}) {
			if len(line) > 0 {
				onStderr(line)
			}
		}
	}
	werr := cmd.Wait()
	if ctx.Err() != nil {
		return -1, fmt.Errorf("agent exec cancelled: %w", ctx.Err())
	}
	if werr != nil {
		var ee *exec.ExitError
		if errors.As(werr, &ee) {
			return ee.ExitCode(), nil
		}
		return -1, fmt.Errorf("agent exec: %v (%s)", werr, strings.TrimSpace(stderr.String()))
	}
	return 0, nil
}

// runProvisionPlan starts cmd (a `provision apply <plan> --json` invocation)
// and decodes each stdout line as a provisionEvent, calling onEvent for each
// step line as it arrives and returning the trailing summary line once the
// process exits. Mirrors streamCommand's ReadBytes loop (no 64 KB
// bufio.Scanner cap — same class of "a line arriving mid-write" problem) and
// its stderr-after-stdout draining, but decodes each line as it goes rather
// than handing raw bytes to a callback: the runner needs the parsed event,
// not the text.
//
// A non-zero exit (apply's own "a step failed", exit 1) is expected and
// reported via the streamed step/summary events, not an executor error —
// mirrors the old parseStepResult's "mooncake prints JSON even on failure"
// rule. Only a failure to start/run the process, an unparseable line, a
// missing summary line, or a cancelled context is an executor error.
func runProvisionPlan(ctx context.Context, cmd *exec.Cmd, onEvent func(provisionEvent)) (provisionEvent, error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return provisionEvent{}, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return provisionEvent{}, err
	}
	if err := cmd.Start(); err != nil {
		return provisionEvent{}, err
	}
	// Accumulate stderr off-goroutine; only surfaced in an error message.
	var stderr bytes.Buffer
	var stderrDone sync.WaitGroup
	stderrDone.Add(1)
	go func() {
		defer stderrDone.Done()
		_, _ = io.Copy(&stderr, stderrPipe)
	}()

	var summary provisionEvent
	var sawSummary bool
	r := bufio.NewReader(stdout)
	for {
		line, rerr := r.ReadBytes('\n')
		if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
			var ev provisionEvent
			if jerr := json.Unmarshal(trimmed, &ev); jerr != nil {
				stderrDone.Wait()
				_ = cmd.Wait()
				return provisionEvent{}, fmt.Errorf("provision --json: unparseable line %q: %v (stderr: %s)", trimmed, jerr, strings.TrimSpace(stderr.String()))
			}
			if ev.Event == "summary" {
				summary = ev
				sawSummary = true
			} else {
				onEvent(ev)
			}
		}
		if rerr != nil {
			break // EOF (process closing stdout) or read error; Wait reports the real outcome
		}
	}
	stderrDone.Wait()

	werr := cmd.Wait()
	if ctx.Err() != nil {
		return provisionEvent{}, fmt.Errorf("plan cancelled: %w", ctx.Err())
	}
	if werr != nil {
		var ee *exec.ExitError
		if !errors.As(werr, &ee) {
			return provisionEvent{}, fmt.Errorf("provision apply: %v (stderr: %s)", werr, strings.TrimSpace(stderr.String()))
		}
		// Exit 1 (a step failed) falls through — the failure is already in the
		// streamed events; nothing further to report here.
	}
	if !sawSummary {
		return provisionEvent{}, fmt.Errorf("provision apply: no summary line (stderr: %s)", strings.TrimSpace(stderr.String()))
	}
	return summary, nil
}

// Close removes the container. It uses a fresh background context with a short
// timeout so teardown still runs after a run-timeout has cancelled the parent
// context — otherwise the detached container would leak.
func (d *dockerSession) Close() error {
	removeContainer(d.logger, d.name)
	return nil
}

// removeContainer force-removes a container by name, best-effort. It uses a
// fresh short-lived context so teardown still runs after the parent context was
// cancelled (a run timeout) — otherwise a detached agent container would leak.
// It backs both dockerSession.Close and the agent turn-loop teardown (where the
// container outlives any single session handle).
func removeContainer(logger *slog.Logger, name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "docker", "rm", "-f", name).CombinedOutput(); err != nil {
		logger.Error("container cleanup", "name", name, "err", err, "out", strings.TrimSpace(string(out)))
	}
}

// runClaudeStreamHost runs the agent command on the host (the CIIsolation=none
// path), streaming stdout line-by-line. Agent runs normally use docker; this
// exists so the host session isn't missing the capability.
func runClaudeStreamHost(ctx context.Context, workDir string, argv []string, onLine, onStderr func(line []byte)) (int, error) {
	if len(argv) == 0 {
		return -1, errors.New("empty agent command")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = workDir
	return streamCommand(ctx, cmd, onLine, onStderr)
}

// sweepOrphanContainers removes any codefort-ci-* or codefort-agent-* containers
// left behind by a crashed runner. The two name filters are OR'd by docker, so
// both CI job containers and agent containers are reaped. Best-effort:
// failures are logged, not fatal.
func sweepOrphanContainers(ctx context.Context, logger *slog.Logger) {
	out, err := exec.CommandContext(ctx, "docker", "ps", "-aq",
		"--filter", "name=codefort-ci-", "--filter", "name=codefort-agent-").Output()
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
	return fmt.Sprintf("codefort-ci-%d-%s", jobID, sanitizeContainerName(jobName))
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

// hostPath translates a container-side path under DataDir to the corresponding
// host-side path under HostDataDir. When codefortd runs inside Docker, DataDir
// is the in-container mount point (e.g. /data) but sibling CI/agent containers
// are launched by the host Docker daemon, which resolves bind-mount sources on
// the HOST filesystem. HostDataDir holds the host-side path (e.g.
// /home/user/.local/share/codefort); the substitution makes the workspace
// visible inside sibling containers. No-op when the two dirs are identical
// (non-containerised deployments).
func (r *ciRunner) hostPath(containerPath string) string {
	if r.cfg.HostDataDir == r.cfg.DataDir {
		return containerPath
	}
	if strings.HasPrefix(containerPath, r.cfg.DataDir) {
		return r.cfg.HostDataDir + containerPath[len(r.cfg.DataDir):]
	}
	return containerPath
}

// runProvisionPlanHost executes one job's provision plan via `provision apply
// <planFile> --json` in workDir. It backs the host session.
func runProvisionPlanHost(ctx context.Context, workDir, planFile string, onEvent func(provisionEvent)) (provisionEvent, error) {
	cmd := exec.CommandContext(ctx, "provision", "apply", planFile, "--json")
	cmd.Dir = workDir
	return runProvisionPlan(ctx, cmd, onEvent)
}

// gitCheckout materializes the repo tree at commitSHA into workDir as a real
// working tree with a populated .git. A bare `git archive | tar` extract is
// lighter, but quality gates that shell out to git fail in a .git-less tree —
// go-quality's full.sh does `cd "$(git rev-parse --show-toplevel)"`, and
// arch-snapshot/dupl walk the repo — so CI now gets a clone.
//
// `--local` hardlinks objects from the sibling bare repo (no copy, no network);
// `--no-checkout` then a detached checkout pins the exact commit without
// populating the default branch first. workDir is freshly created and empty
// (see executeRun), which `git clone` requires.
func gitCheckout(ctx context.Context, bareRepo, commitSHA, workDir string) error {
	clone := exec.CommandContext(ctx, "git", "clone", "--quiet", "--local", "--no-checkout", bareRepo, workDir)
	if out, err := clone.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	checkout := exec.CommandContext(ctx, "git", "-C", workDir, "checkout", "--quiet", "--detach", commitSHA)
	if out, err := checkout.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout %s: %v (%s)", commitSHA, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// gitReadPipeline reads codefort.yml at commitSHA from the bare repo. A missing
// file (git reports the path doesn't exist at that rev) yields ok=false rather
// than an error — that's the gate for "this commit has no pipeline".
func gitReadPipeline(ctx context.Context, bareRepo, commitSHA string) ([]byte, bool, error) {
	cmd := exec.CommandContext(ctx, "git", "--git-dir", bareRepo, "show", commitSHA+":codefort.yml")
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
