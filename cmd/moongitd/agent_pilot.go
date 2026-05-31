package main

import (
	"bytes"
	"encoding/json"
	"strconv"

	"github.com/alehatsman/moongit/internal/ci"
	"github.com/alehatsman/moongit/internal/config"
)

// pilotMaxIterationsDefault is the fallback iteration cap when config
// doesn't set one (or sets a non-positive value).
const pilotMaxIterationsDefault = 10

// pilotExecutor is the mooncake-pilot model: each turn runs
// `mooncake pilot run` inside the agent container. Claude is used only as
// a planner (provider anthropic-cli, reusing the injected
// CLAUDE_CODE_OAUTH_TOKEN); mooncake validates and APPLIES the plan, so
// commands (shell/git/tests) execute under mooncake's control rather than
// claude's policy-gated Bash tool (#110). It edits /work like claude-edit
// does, so the same server-side handoff materializes the branch.
type pilotExecutor struct {
	maxIterations int
}

func newPilotExecutor(cfg *config.Config) *pilotExecutor {
	iters := pilotMaxIterationsDefault
	if cfg != nil && cfg.AgentPilotMaxIterations > 0 {
		iters = cfg.AgentPilotMaxIterations
	}
	return &pilotExecutor{maxIterations: iters}
}

func (*pilotExecutor) Model() string { return agentModelMooncakePilot }

// Argv builds the pilot invocation for a turn. spec.prompt (the issue body
// on turn 1, a follow-up message thereafter) is the goal; pilot has no
// resumable session, so the persisted /work checkout is the carried state
// and sessionID/systemPrompt/resume are unused. --output-format json gives
// the NDJSON event stream Translate parses; --auto-apply runs unattended.
func (p *pilotExecutor) Argv(spec turnSpec) []string {
	return []string{
		"mooncake", "pilot", "run",
		"--goal", spec.prompt,
		"--provider", "anthropic-cli",
		"--style", "plan",
		"--auto-apply",
		"--max-iterations", strconv.Itoa(p.maxIterations),
		"--output-format", "json",
	}
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
