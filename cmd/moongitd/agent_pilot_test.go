package main

import (
	"testing"

	"github.com/alehatsman/moongit/internal/ci"
	"github.com/alehatsman/moongit/internal/config"
)

func TestPilotExecutorArgv(t *testing.T) {
	p := newPilotExecutor(&config.Config{
		AgentPilotMaxIterations: 7,
		AgentPilotDenyActions:   []string{"shell", "cmd"},
		AgentPilotAllowActions:  []string{"file.write"},
		AgentPilotDenyNetwork:   true,
		AgentPilotMaxRisk:       6,
	})
	if p.Model() != agentModelMooncakePilot {
		t.Fatalf("Model() = %q, want %q", p.Model(), agentModelMooncakePilot)
	}
	argv := p.Argv(turnSpec{prompt: "fix the bug", sessionID: "ignored", resume: true})
	if argv[0] != "mooncake" || argv[1] != "pilot" || argv[2] != "run" {
		t.Errorf("argv prefix = %v, want mooncake pilot run", argv[:3])
	}
	if !argvHas(argv, "--goal", "fix the bug") {
		t.Errorf("missing --goal: %v", argv)
	}
	if !argvHas(argv, "--provider", "anthropic-cli") {
		t.Errorf("missing --provider anthropic-cli: %v", argv)
	}
	if !argvHas(argv, "--output-format", "json") {
		t.Errorf("missing --output-format json: %v", argv)
	}
	if !argvContains(argv, "--auto-apply") {
		t.Errorf("missing --auto-apply: %v", argv)
	}
	if !argvHas(argv, "--max-iterations", "7") {
		t.Errorf("max-iterations not threaded from config: %v", argv)
	}
	// Policy flags (#11) threaded from config.
	if !argvHas(argv, "--deny-action", "shell") || !argvHas(argv, "--deny-action", "cmd") {
		t.Errorf("missing --deny-action shell/cmd: %v", argv)
	}
	if !argvHas(argv, "--allow-action", "file.write") {
		t.Errorf("missing --allow-action file.write: %v", argv)
	}
	if !argvContains(argv, "--deny-network") {
		t.Errorf("missing --deny-network: %v", argv)
	}
	if !argvHas(argv, "--max-risk", "6") {
		t.Errorf("missing --max-risk 6: %v", argv)
	}
}

func TestPilotExecutorArgvNoPolicy(t *testing.T) {
	// No policy configured → no policy flags emitted (ungated, operator's
	// explicit choice via empty deny list).
	argv := newPilotExecutor(&config.Config{}).Argv(turnSpec{prompt: "g"})
	for _, f := range []string{"--deny-action", "--allow-action", "--deny-network", "--max-risk"} {
		if argvContains(argv, f) {
			t.Errorf("unexpected %s with empty policy: %v", f, argv)
		}
	}
}

func TestPilotExecutorArgvDefaultIterations(t *testing.T) {
	// nil cfg / non-positive value falls back to the built-in default.
	if argv := newPilotExecutor(nil).Argv(turnSpec{prompt: "g"}); !argvHas(argv, "--max-iterations", "10") {
		t.Errorf("nil cfg should default to 10 iterations: %v", argv)
	}
	if argv := newPilotExecutor(&config.Config{AgentPilotMaxIterations: 0}).Argv(turnSpec{prompt: "g"}); !argvHas(argv, "--max-iterations", "10") {
		t.Errorf("zero iterations should default to 10: %v", argv)
	}
}

func TestPilotTranslate(t *testing.T) {
	var p pilotExecutor

	t.Run("step event passes through under mooncake", func(t *testing.T) {
		et, data, res := p.Translate([]byte(`{"type":"step.started","data":{"action":"shell"}}`))
		if et != ci.EventAgentMessage {
			t.Fatalf("type = %q, want %q", et, ci.EventAgentMessage)
		}
		if res != nil {
			t.Errorf("non-terminal event returned a result: %+v", res)
		}
		mc, ok := data["mooncake"].(map[string]any)
		if !ok || mc["type"] != "step.started" {
			t.Errorf("data[mooncake] = %v, want the parsed object", data["mooncake"])
		}
	})

	t.Run("pilot.completed success yields a clean result", func(t *testing.T) {
		_, _, res := p.Translate([]byte(`{"type":"pilot.completed","data":{"status":"success","stop_reason":"success","iterations":2}}`))
		if res == nil {
			t.Fatal("pilot.completed returned nil result")
		}
		if res.IsError || res.Subtype != "success" {
			t.Errorf("result = %+v, want success/no-error", res)
		}
	})

	t.Run("pilot.completed failure marks error", func(t *testing.T) {
		_, _, res := p.Translate([]byte(`{"type":"pilot.completed","data":{"status":"failed","stop_reason":"failed"}}`))
		if res == nil || !res.IsError {
			t.Errorf("failed pilot.completed should mark IsError: %+v", res)
		}
	})

	t.Run("non-JSON line preserved as agent.raw", func(t *testing.T) {
		et, data, _ := p.Translate([]byte("AutoApply: skipping confirm\n"))
		if et != ci.EventAgentRaw || data["line"] != "AutoApply: skipping confirm" {
			t.Errorf("raw passthrough wrong: et=%q data=%v", et, data)
		}
	})

	t.Run("blank line skipped", func(t *testing.T) {
		if et, _, _ := p.Translate([]byte("  \n")); et != "" {
			t.Errorf("blank line type = %q, want empty (skip)", et)
		}
	})
}
