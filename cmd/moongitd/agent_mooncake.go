package main

import (
	"bytes"
	"encoding/json"
	"strconv"
	"time"

	"github.com/alehatsman/moongit/internal/ci"
	"github.com/alehatsman/moongit/internal/config"
)

// mooncakeMaxIterationsDefault is the run-until-done backstop, not a working
// budget. Under --style step (one action per iteration) a real fix needs many
// iterations — read, edit, verify, react — so the loop is meant to end on its
// OWN terminal signal: step_done (the planner emits an empty plan = "goal
// reached") or no_progress (a stall). mooncake has no unbounded mode — 0 or a
// negative value resets to its built-in 5 — so we pass a high explicit cap and
// let the per-turn wall-clock (AgentTurnTimeout, enforced as a process kill in
// runAgentTurn) be the real ceiling. The cap only catches a planner that never
// converges yet never stalls. Operators can still pin a lower limit via
// MOONGIT_AGENT_MOONCAKE_MAX_ITERATIONS.
const mooncakeMaxIterationsDefault = 100

// mooncakeExecutor is the mooncake-agent model: each turn runs
// `mooncake agent run` inside the agent container. Claude is used only as
// a planner (provider anthropic-cli, reusing the injected
// CLAUDE_CODE_OAUTH_TOKEN); mooncake validates and APPLIES the plan, so
// commands (shell/git/tests) execute under mooncake's control rather than
// claude's policy-gated Bash tool (#110). It edits /work like claude-edit
// does, so the same server-side handoff materializes the branch.
type mooncakeExecutor struct {
	maxIterations int
	// Policy (#110/#11): mooncake enforces these per run at preflight, so a
	// denied step fails before any side effect. This is the execution wall
	// that moving off Claude's managed Bash policy would otherwise lose.
	allowActions []string
	denyActions  []string
	denyNetwork  bool
	maxRisk      int
	// llmTimeout aligns mooncake's per-iteration plan-generation cap with our
	// own per-turn budget (#176). mooncake's built-in default is 5m, which can
	// SIGKILL a thinking-heavy planner ("claude CLI failed: signal: killed")
	// long before our AgentTurnTimeout would fire. Passing AgentTurnTimeout as
	// --llm-timeout makes mooncake's internal cap never stricter than ours, so
	// our context deadline stays the real governor. Zero = don't pass the flag
	// (keep mooncake's default).
	llmTimeout time.Duration
}

// newMooncakeExecutor builds the mooncake executor from config. allowShell is the
// run's spawn-time override: when set, shell/cmd are dropped from the policy's
// deny list for this run, letting the agent's plan run shell commands (#110).
func newMooncakeExecutor(cfg *config.Config, allowShell bool) *mooncakeExecutor {
	p := &mooncakeExecutor{maxIterations: mooncakeMaxIterationsDefault}
	if cfg != nil {
		if cfg.AgentMooncakeMaxIterations > 0 {
			p.maxIterations = cfg.AgentMooncakeMaxIterations
		}
		p.allowActions = cfg.AgentMooncakeAllowActions
		p.denyActions = cfg.AgentMooncakeDenyActions
		p.denyNetwork = cfg.AgentMooncakeDenyNetwork
		p.maxRisk = cfg.AgentMooncakeMaxRisk
		p.llmTimeout = cfg.AgentTurnTimeout
	}
	if allowShell {
		p.denyActions = withoutShellCmd(p.denyActions)
	}
	return p
}

// withoutShellCmd returns deny with "shell" and "cmd" removed, preserving any
// other denied actions. Used by the per-run allow-shell override.
func withoutShellCmd(deny []string) []string {
	var out []string
	for _, a := range deny {
		if a == "shell" || a == "cmd" {
			continue
		}
		out = append(out, a)
	}
	return out
}

func (*mooncakeExecutor) Model() string { return agentModelMooncakeAgent }

// mooncakeGoalPreamble orients the mooncake planner toward acting instead of
// deliberating. The claude-edit system prompt can't be reused here — it tells
// the agent shell is unavailable, which is false under mooncake (and doubly so
// with allow_shell). Paired with --style step, it nudges the planner to commit
// to one concrete next action rather than authoring (and re-authoring) a long
// plan up front, which is the behavior that had runs burn a whole turn thinking.
const mooncakeGoalPreamble = "You are an autonomous coding agent resolving a task in the /work checkout. " +
	"Work in small concrete steps: take the single next action that makes real progress " +
	"— read a file, make an edit, run a check — then react to its result and continue. " +
	"Don't draft a long plan or deliberate at length up front; act, observe, and keep going " +
	"until the task is genuinely done, then stop. Stay within the task's scope.\n\n"

// composeMooncakeGoal wraps a turn's work text in the working-style preamble.
// It rides every turn (turn 1's issue and later follow-ups alike) because
// mooncake has no session to carry orientation across turns.
func composeMooncakeGoal(goal string) string {
	return mooncakeGoalPreamble + goal
}

// Argv builds the mooncake invocation for a turn. The goal is the turn's work
// (issue body on turn 1, follow-up message after) wrapped in a working-style
// preamble (composeMooncakeGoal); mooncake has no resumable session, so the
// persisted /work checkout is the carried state and sessionID/resume are unused.
// --style step makes the planner emit ONE action per iteration and re-plan with
// its result, so it works incrementally toward done instead of authoring a whole
// plan up front (which had it deliberating for minutes without acting). It rides
// every turn since there's no session to carry it. --output-format json gives the
// NDJSON event stream Translate parses; --auto-apply runs unattended.
func (p *mooncakeExecutor) Argv(in turnInput) []string {
	argv := []string{
		"mooncake", "agent", "run",
		"--goal", composeMooncakeGoal(in.goal()),
		"--provider", "anthropic-cli",
		"--style", "step",
		"--auto-apply",
		"--max-iterations", strconv.Itoa(p.maxIterations),
		"--output-format", "json",
	}
	// Align mooncake's per-iteration plan-gen cap with our turn budget (#176)
	// so its 5m default can't SIGKILL a long planner before AgentTurnTimeout.
	if p.llmTimeout > 0 {
		argv = append(argv, "--llm-timeout", p.llmTimeout.String())
	}
	// Policy flags (#11): re-establish the execution wall under mooncake's
	// control. deny wins over allow; a denied/over-risk/egress step fails the
	// run before any side effect.
	for _, a := range p.allowActions {
		argv = append(argv, "--allow-action", a)
	}
	for _, a := range p.denyActions {
		argv = append(argv, "--deny-action", a)
	}
	if p.denyNetwork {
		argv = append(argv, "--deny-network")
	}
	if p.maxRisk > 0 {
		argv = append(argv, "--max-risk", strconv.Itoa(p.maxRisk))
	}
	return argv
}

// Translate maps one line of mooncake's NDJSON event stream onto an
// agent-transcript event. Schema-tolerant like the claude translator: it
// keys only on the top-level "type" and carries the whole object verbatim
// under Data["mooncake"], so the renderer decides how to display each
// kind. A non-JSON line is preserved as agent.raw; a blank line is
// skipped. The terminal agent.completed event yields a turnResult so the
// caller can finalize the turn.
func (*mooncakeExecutor) Translate(line []byte) (string, map[string]any, *turnResult) {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return "", nil, nil
	}
	var obj map[string]any
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return ci.EventAgentRaw, map[string]any{"line": string(trimmed)}, nil
	}
	data := map[string]any{"mooncake": obj}
	if t, _ := obj["type"].(string); t == "agent.completed" {
		return ci.EventAgentMessage, data, mooncakeTurnResult(obj["data"])
	}
	return ci.EventAgentMessage, data, nil
}

// mooncakeTurnResult distills an agent.completed event's Data into the
// model-agnostic turnResult. status carries the worst iteration outcome and
// stop_reason carries why the loop stopped; both ride through so turnStatus can
// distinguish a hard failure from a no-progress stall (moongit #173). IsError
// means a step genuinely failed — a non-clean status (anything but success/
// step_done, mooncake's two severity-0 outcomes) or an explicit "failed" stop.
// A clean status with a soft stop (max_iterations/no_progress) is NOT an error
// here; turnStatus maps it to "stalled". mooncake doesn't report claude's
// per-turn cost/num_turns, so those stay zero.
func mooncakeTurnResult(raw any) *turnResult {
	res := &turnResult{}
	data, ok := raw.(map[string]any)
	if !ok {
		return res
	}
	status, _ := data["status"].(string)
	stop, _ := data["stop_reason"].(string)
	res.Subtype = status
	res.StopReason = stop
	res.IsError = (status != "" && status != "success" && status != "step_done") || stop == "failed"
	return res
}
