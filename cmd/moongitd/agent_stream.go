package main

import (
	"bytes"
	"encoding/json"

	"github.com/alehatsman/moongit/internal/ci"
)

// claudeResult is the subset of claude's terminal stream-json "result" object
// the runner needs to finalize a turn. claude prints exactly one of these last,
// whether the turn succeeded or hit a limit/error.
type claudeResult struct {
	Subtype      string  `json:"subtype"` // "success" | "error_max_turns" | "error_during_execution" | ...
	IsError      bool    `json:"is_error"`
	NumTurns     int     `json:"num_turns"`
	DurationMS   int     `json:"duration_ms"`
	TotalCostUSD float64 `json:"total_cost_usd"`
}

// translateClaudeLine maps one line of claude's stream-json (NDJSON) output onto
// an agent-transcript event. It is deliberately schema-tolerant: it keys only
// on the stable top-level "type" and carries the whole parsed object verbatim
// under Data["claude"], so the renderer decides how to display each kind and
// nothing breaks if Claude's inner schema evolves. A line that isn't valid JSON
// is preserved as agent.raw rather than dropped. A blank line yields an empty
// eventType, signaling the caller to skip it. When the line is the terminal
// "result" object, result is returned non-nil so the caller can finalize.
func translateClaudeLine(line []byte) (eventType string, data map[string]any, result *claudeResult) {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return "", nil, nil
	}
	var obj map[string]any
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return ci.EventAgentRaw, map[string]any{"line": string(trimmed)}, nil
	}
	if t, _ := obj["type"].(string); t == "result" {
		var r claudeResult
		// Best-effort: a result object always parses, but tolerate partials.
		_ = json.Unmarshal(trimmed, &r)
		result = &r
	}
	return ci.EventAgentMessage, map[string]any{"claude": obj}, result
}

// turnStatus maps a finished turn onto a CI-style status string for the
// turn.completed event and run finalization. A missing result (the CLI died
// before emitting a terminal record) or a non-zero exit is an error; an
// is_error/non-success result is a failure; otherwise success. Operates on the
// model-agnostic turnResult so it serves every executor.
func turnStatus(result *turnResult, exitCode int, execErr error) string {
	switch {
	case execErr != nil || exitCode != 0 || result == nil:
		return "error"
	case result.IsError || (result.Subtype != "" && result.Subtype != "success"):
		return "failed"
	default:
		return "success"
	}
}
