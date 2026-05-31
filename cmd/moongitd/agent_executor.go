package main

import (
	"fmt"

	"github.com/alehatsman/moongit/internal/config"
	"github.com/alehatsman/moongit/internal/storage"
)

// Execution models an agent run can use. The model is chosen per run
// (stored on the run; see migration 11) and decides what actually runs
// inside the agent container for each turn — everything else (container,
// workspace, token, dex, event log, parking, handoff) is shared. The
// identifiers are canonical in storage so storage/server/runner agree.
const (
	// agentModelClaudeEdit runs `claude -p` directly. Claude edits files
	// in /work with its file tools; its Bash/execution tools are
	// policy-gated under subscription auth and not reliably unlockable
	// headlessly (#110), so it can't run commands.
	agentModelClaudeEdit = storage.ExecModelClaudeEdit
	// agentModelMooncakePilot runs `mooncake pilot run`. Claude is used
	// only as a planner (text completion → a mooncake plan); mooncake
	// itself applies the actions, so commands (tests, git, shell) run
	// under mooncake's control rather than claude's gated tool-use.
	agentModelMooncakePilot = storage.ExecModelMooncakePilot
)

// turnSpec is the per-turn input an executor turns into a command. The
// same struct serves both models; an executor ignores the fields that
// don't apply to it (pilot ignores sessionID/systemPrompt/resume — it
// has no resumable session and builds its own prompt from the goal).
type turnSpec struct {
	sessionID    string // claude session UUID (claude-edit only)
	prompt       string // the turn's user message / goal (issue body on turn 1)
	systemPrompt string // moongit-authored framing (claude-edit, turn 1 only)
	mcpPath      string // dex MCP config path, "" to omit
	resume       bool   // follow-up turn: resume the session (claude-edit only)
}

// turnResult is the model-agnostic outcome of one turn, distilled from
// whatever terminal record the underlying CLI emits (claude's stream-json
// "result" object, or mooncake's pilot.completed event). Fields a given
// model doesn't report stay zero.
type turnResult struct {
	IsError      bool
	Subtype      string  // "success" | error subtype; "" when not reported
	NumTurns     int     // claude only
	DurationMS   int     // claude only
	TotalCostUSD float64 // claude only
}

// agentExecutor is the per-model seam: it builds the command for a turn
// and translates each raw output line into an agent-transcript event. The
// runner (agent_runner.go) drives it identically regardless of model.
type agentExecutor interface {
	// Model returns the execution-model identifier this executor serves.
	Model() string
	// Argv builds the argv to exec inside the agent container for one turn.
	Argv(spec turnSpec) []string
	// Translate maps one raw output line onto an agent event. eventType
	// "" signals "skip this line". A non-nil result marks the terminal
	// record the caller uses to finalize the turn.
	Translate(line []byte) (eventType string, data map[string]any, result *turnResult)
}

// newAgentExecutor selects the executor for a run's model. An empty model
// is the default (claude-edit); an unknown model is an error so a bad
// value fails the run loudly rather than silently picking a default.
func newAgentExecutor(model string, cfg *config.Config) (agentExecutor, error) {
	switch model {
	case "", agentModelClaudeEdit:
		return &claudeExecutor{}, nil
	case agentModelMooncakePilot:
		return newPilotExecutor(cfg), nil
	default:
		return nil, fmt.Errorf("unknown agent execution model %q", model)
	}
}

// claudeExecutor is the claude-edit model: it wraps the existing claude
// argv builder and stream-json translator behind the executor seam.
type claudeExecutor struct{}

func (claudeExecutor) Model() string { return agentModelClaudeEdit }

func (claudeExecutor) Argv(spec turnSpec) []string {
	return buildClaudeArgv(spec.sessionID, spec.prompt, spec.systemPrompt, spec.mcpPath, spec.resume)
}

func (claudeExecutor) Translate(line []byte) (string, map[string]any, *turnResult) {
	et, data, cr := translateClaudeLine(line)
	if cr == nil {
		return et, data, nil
	}
	return et, data, &turnResult{
		IsError:      cr.IsError,
		Subtype:      cr.Subtype,
		NumTurns:     cr.NumTurns,
		DurationMS:   cr.DurationMS,
		TotalCostUSD: cr.TotalCostUSD,
	}
}
