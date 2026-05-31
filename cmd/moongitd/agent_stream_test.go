package main

import (
	"testing"

	"github.com/alehatsman/moongit/internal/ci"
)

func TestTranslateClaudeLine(t *testing.T) {
	t.Run("assistant message passes through under claude", func(t *testing.T) {
		et, data, res := translateClaudeLine([]byte(`{"type":"assistant","message":{"role":"assistant"}}` + "\n"))
		if et != ci.EventAgentMessage {
			t.Fatalf("type = %q, want %q", et, ci.EventAgentMessage)
		}
		if res != nil {
			t.Errorf("non-result line returned a result: %+v", res)
		}
		claude, ok := data["claude"].(map[string]any)
		if !ok || claude["type"] != "assistant" {
			t.Errorf("data[claude] = %v, want the parsed object", data["claude"])
		}
	})

	t.Run("result line yields a result for finalization", func(t *testing.T) {
		_, _, res := translateClaudeLine([]byte(`{"type":"result","subtype":"success","is_error":false,"num_turns":2,"duration_ms":99}`))
		if res == nil {
			t.Fatal("result line returned nil result")
		}
		if res.Subtype != "success" || res.IsError || res.NumTurns != 2 || res.DurationMS != 99 {
			t.Errorf("result = %+v, want success/2/99", res)
		}
	})

	t.Run("non-JSON line is preserved as agent.raw", func(t *testing.T) {
		et, data, _ := translateClaudeLine([]byte("warning: something\n"))
		if et != ci.EventAgentRaw {
			t.Fatalf("type = %q, want %q", et, ci.EventAgentRaw)
		}
		if data["line"] != "warning: something" {
			t.Errorf("line = %q, want preserved text", data["line"])
		}
	})

	t.Run("blank line is skipped", func(t *testing.T) {
		et, _, _ := translateClaudeLine([]byte("   \n"))
		if et != "" {
			t.Errorf("blank line type = %q, want empty (skip)", et)
		}
	})
}

func TestTurnStatus(t *testing.T) {
	cases := []struct {
		name    string
		result  *turnResult
		exit    int
		execErr error
		want    string
	}{
		{"clean success", &turnResult{Subtype: "success"}, 0, nil, "success"},
		{"error result", &turnResult{Subtype: "error_max_turns", IsError: true}, 0, nil, "failed"},
		{"is_error flag", &turnResult{Subtype: "success", IsError: true}, 0, nil, "failed"},
		{"non-zero exit", &turnResult{Subtype: "success"}, 1, nil, "error"},
		{"missing result", nil, 0, nil, "error"},
		{"exec error", nil, 0, errTest("boom"), "error"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := turnStatus(c.result, c.exit, c.execErr); got != c.want {
				t.Errorf("turnStatus = %q, want %q", got, c.want)
			}
		})
	}
}
