package main

import (
	"bytes"
	"encoding/json"
	"strconv"

	"github.com/alehatsman/moongit/internal/ci"
	"github.com/alehatsman/moongit/internal/config"
)

// pilotMaxIterationsDefault is the fallback iteration cap when config
// doesn't set one (or sets a non-positive value). Kept low: on a
// deterministic step failure the pilot re-runs the whole regenerated plan
// each iteration rather than adapting, so a high cap just burns minutes
// re-failing the same step (dex run #21 ground through 10 ≈ 9 min). 3 gives
// the planner a couple of genuine retries before giving up; raise via
// MOONGIT_AGENT_PILOT_MAX_ITERATIONS for tasks that legitimately need more.
const pilotMaxIterationsDefault = 3

// pilotExecutor is the mooncake-pilot model: each turn runs
// `mooncake pilot run` inside the agent container. Claude is used only as
// a planner (provider anthropic-cli, reusing the injected
// CLAUDE_CODE_OAUTH_TOKEN); mooncake validates and APPLIES the plan, so
// commands (shell/git/tests) execute under mooncake's control rather than
// claude's policy-gated Bash tool (#110). It edits /work like claude-edit
// does, so the same server-side handoff materializes the branch.
type pilotExecutor struct {
	maxIterations int
	// Policy (#110/#11): mooncake enforces these per run at preflight, so a
	// denied step fails before any side effect. This is the execution wall
	// that moving off Claude's managed Bash policy would otherwise lose.
	allowActions []string
	denyActions  []string
	denyNetwork  bool
	maxRisk      int
}

// newPilotExecutor builds the pilot executor from config. allowShell is the
// run's spawn-time override: when set, shell/cmd are dropped from the policy's
// deny list for this run, letting the agent's plan run shell commands (#110).
func newPilotExecutor(cfg *config.Config, allowShell bool) *pilotExecutor {
	p := &pilotExecutor{maxIterations: pilotMaxIterationsDefault}
	if cfg != nil {
		if cfg.AgentPilotMaxIterations > 0 {
			p.maxIterations = cfg.AgentPilotMaxIterations
		}
		p.allowActions = cfg.AgentPilotAllowActions
		p.denyActions = cfg.AgentPilotDenyActions
		p.denyNetwork = cfg.AgentPilotDenyNetwork
		p.maxRisk = cfg.AgentPilotMaxRisk
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

func (*pilotExecutor) Model() string { return agentModelMooncakePilot }

// Argv builds the pilot invocation for a turn. in.goal() (the issue body on
// turn 1, a follow-up message thereafter) is the goal; pilot has no resumable
// session, so the persisted /work checkout is the carried state and
// sessionID/resume are unused (and no system prompt is composed for it).
// --output-format json gives the NDJSON event stream Translate parses;
// --auto-apply runs unattended.
func (p *pilotExecutor) Argv(in turnInput) []string {
	argv := []string{
		"mooncake", "pilot", "run",
		"--goal", in.goal(),
		"--provider", "anthropic-cli",
		"--style", "plan",
		"--auto-apply",
		"--max-iterations", strconv.Itoa(p.maxIterations),
		"--output-format", "json",
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
// skipped. The terminal pilot.completed event yields a turnResult so the
// caller can finalize the turn.
func (*pilotExecutor) Translate(line []byte) (string, map[string]any, *turnResult) {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return "", nil, nil
	}
	var obj map[string]any
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return ci.EventAgentRaw, map[string]any{"line": string(trimmed)}, nil
	}
	data := map[string]any{"mooncake": obj}
	if t, _ := obj["type"].(string); t == "pilot.completed" {
		return ci.EventAgentMessage, data, pilotTurnResult(obj["data"])
	}
	return ci.EventAgentMessage, data, nil
}

// pilotTurnResult distills a pilot.completed event's Data into the
// model-agnostic turnResult. The status field carries the iteration
// outcome; a non-"success" status (or a "failed" stop_reason) marks the
// turn failed. pilot doesn't report claude's per-turn cost/num_turns, so
// those stay zero.
func pilotTurnResult(raw any) *turnResult {
	res := &turnResult{}
	data, ok := raw.(map[string]any)
	if !ok {
		return res
	}
	status, _ := data["status"].(string)
	stop, _ := data["stop_reason"].(string)
	res.Subtype = status
	res.IsError = (status != "" && status != "success") || stop == "failed"
	return res
}
