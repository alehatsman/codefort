package main

import (
	"testing"

	"github.com/alehatsman/moongit/internal/ci"
	"github.com/alehatsman/moongit/internal/config"
)

func TestMooncakeExecutorArgv(t *testing.T) {
	p := newMooncakeExecutor(&config.Config{
		AgentMooncakeMaxIterations: 7,
		AgentMooncakeDenyActions:   []string{"shell", "cmd"},
		AgentMooncakeAllowActions:  []string{"file.write"},
		AgentMooncakeDenyNetwork:   true,
		AgentMooncakeMaxRisk:       6,
	}, false)
	if p.Model() != agentModelMooncakeAgent {
		t.Fatalf("Model() = %q, want %q", p.Model(), agentModelMooncakeAgent)
	}
	argv := p.Argv(turnInput{message: "fix the bug", sessionID: "ignored", resume: true})
	if argv[0] != "mooncake" || argv[1] != "agent" || argv[2] != "run" {
		t.Errorf("argv prefix = %v, want mooncake agent run", argv[:3])
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

func TestMooncakeExecutorArgvNoPolicy(t *testing.T) {
	// No policy configured → no policy flags emitted (ungated, operator's
	// explicit choice via empty deny list).
	argv := newMooncakeExecutor(&config.Config{}, false).Argv(turnInput{message: "g"})
	for _, f := range []string{"--deny-action", "--allow-action", "--deny-network", "--max-risk"} {
		if argvContains(argv, f) {
			t.Errorf("unexpected %s with empty policy: %v", f, argv)
		}
	}
}

func TestMooncakeExecutorAllowShellOverride(t *testing.T) {
	cfg := &config.Config{AgentMooncakeDenyActions: []string{"shell", "cmd", "os.shutdown"}}
	// allowShell=false keeps the default denial.
	denied := newMooncakeExecutor(cfg, false).Argv(turnInput{message: "g"})
	if !argvHas(denied, "--deny-action", "shell") || !argvHas(denied, "--deny-action", "cmd") {
		t.Errorf("allowShell=false should keep shell/cmd denied: %v", denied)
	}
	// allowShell=true drops only shell/cmd; other denies survive.
	allowed := newMooncakeExecutor(cfg, true).Argv(turnInput{message: "g"})
	if argvHas(allowed, "--deny-action", "shell") || argvHas(allowed, "--deny-action", "cmd") {
		t.Errorf("allowShell=true should drop shell/cmd denial: %v", allowed)
	}
	if !argvHas(allowed, "--deny-action", "os.shutdown") {
		t.Errorf("allowShell=true must preserve other denies: %v", allowed)
	}
}

func TestMooncakeExecutorArgvDefaultIterations(t *testing.T) {
	// nil cfg / non-positive value falls back to the built-in default.
	if argv := newMooncakeExecutor(nil, false).Argv(turnInput{message: "g"}); !argvHas(argv, "--max-iterations", "3") {
		t.Errorf("nil cfg should default to 3 iterations: %v", argv)
	}
	if argv := newMooncakeExecutor(&config.Config{AgentMooncakeMaxIterations: 0}, false).Argv(turnInput{message: "g"}); !argvHas(argv, "--max-iterations", "3") {
		t.Errorf("zero iterations should default to 3: %v", argv)
	}
}

func TestMooncakeTranslate(t *testing.T) {
	var p mooncakeExecutor

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

	t.Run("agent.completed success yields a clean result", func(t *testing.T) {
		_, _, res := p.Translate([]byte(`{"type":"agent.completed","data":{"status":"success","stop_reason":"success","iterations":2}}`))
		if res == nil {
			t.Fatal("agent.completed returned nil result")
		}
		if res.IsError || res.Subtype != "success" {
			t.Errorf("result = %+v, want success/no-error", res)
		}
	})

	t.Run("agent.completed failure marks error", func(t *testing.T) {
		_, _, res := p.Translate([]byte(`{"type":"agent.completed","data":{"status":"failed","stop_reason":"failed"}}`))
		if res == nil || !res.IsError {
			t.Errorf("failed agent.completed should mark IsError: %+v", res)
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
