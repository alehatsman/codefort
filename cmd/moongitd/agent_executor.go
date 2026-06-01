package main

import (
	"fmt"

	"github.com/alehatsman/moongit/internal/api"
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
	// agentModelMooncakeAgent runs `mooncake agent run`. Claude is used
	// only as a planner (text completion → a mooncake plan); mooncake
	// itself applies the actions, so commands (tests, git, shell) run
	// under mooncake's control rather than claude's gated tool-use.
	agentModelMooncakeAgent = storage.ExecModelMooncakeAgent
)

// turnInput is the raw, model-agnostic context for one turn; the executor
// composes its own command from it. The runner fills it with facts (whose
// issue, which follow-up message, the session id) and stays out of prompt
// composition — so a model-specific prompt never has to be computed for a
// model that won't use it. claude-edit frames a system prompt and drives a
// resumable session; mooncake-agent uses the goal text and ignores
// sessionID/resume.
type turnInput struct {
	sessionID string    // claude session UUID (claude-edit; mooncake ignores)
	owner     string    // repo identity, for an executor's framing
	repo      string    // repo identity, for an executor's framing
	issue     api.Issue // the run's issue — the task on the first turn
	message   string    // the follow-up human message; empty on the first turn
	firstTurn bool      // first turn works the issue; later turns work message
	mcpPath   string    // dex MCP config path, "" to omit
	resume    bool      // follow-up turn resumes the session (claude-edit; mooncake ignores)
}

// goal is the turn's user message / goal text: the issue title+body on the
// first turn, the human follow-up message thereafter. Both executors build
// their command on it.
func (in turnInput) goal() string {
	if in.firstTurn {
		return composeTurnPrompt(in.issue)
	}
	return in.message
}

// turnResult is the model-agnostic outcome of one turn, distilled from
// whatever terminal record the underlying CLI emits (claude's stream-json
// "result" object, or mooncake's agent.completed event). Fields a given
// model doesn't report stay zero.
type turnResult struct {
	IsError bool
	Subtype string // "success" | error subtype; "" when not reported
	// StopReason is mooncake's loop stop_reason (max_iterations / no_progress /
	// success / failed / …) when the executor reports one; "" for claude, which
	// has no equivalent. A "soft" stop (the agent ran out of road without a
	// failed step) is surfaced as a "stalled" turn rather than a failure — see
	// turnStatus and moongit #173.
	StopReason   string
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
	// Argv builds the argv to exec inside the agent container for one turn,
	// composing whatever prompt(s) the model needs from the turn input.
	Argv(in turnInput) []string
	// Translate maps one raw output line onto an agent event. eventType
	// "" signals "skip this line". A non-nil result marks the terminal
	// record the caller uses to finalize the turn.
	Translate(line []byte) (eventType string, data map[string]any, result *turnResult)
}

// newAgentExecutor selects the executor for a run's model. An empty model
// is the default (claude-edit); an unknown model is an error so a bad
// value fails the run loudly rather than silently picking a default.
// allowShell is the run's spawn-time override that drops the default
// shell/cmd denial from the mooncake-agent policy (#110); claude-edit ignores it.
func newAgentExecutor(model string, cfg *config.Config, allowShell bool) (agentExecutor, error) {
	switch model {
	case "", agentModelClaudeEdit:
		return &claudeExecutor{}, nil
	case agentModelMooncakeAgent:
		return newMooncakeExecutor(cfg, allowShell), nil
	default:
		return nil, fmt.Errorf("unknown agent execution model %q", model)
	}
}

// claudeExecutor is the claude-edit model: it composes the claude invocation
// (issue/message as the user turn, a moongit-authored system prompt on the
// first turn) and translates its stream-json output behind the executor seam.
type claudeExecutor struct{}

func (claudeExecutor) Model() string { return agentModelClaudeEdit }

func (claudeExecutor) Argv(in turnInput) []string {
	var sys string
	if in.firstTurn {
		// The system prompt orients claude for the whole session, so it rides
		// only the first turn; a --resume turn already carries it.
		sys = composeAgentSystemPrompt(in.owner, in.repo, in.issue)
	}
	return buildClaudeArgv(in.sessionID, in.goal(), sys, in.mcpPath, in.resume)
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
