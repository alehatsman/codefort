package main

import (
	"testing"

	"github.com/alehatsman/moongit/internal/config"
)

func TestNewAgentExecutorSelection(t *testing.T) {
	cfg := &config.Config{}
	cases := []struct {
		model     string
		wantModel string
		wantErr   bool
	}{
		{"", agentModelClaudeEdit, false}, // empty → default
		{agentModelClaudeEdit, agentModelClaudeEdit, false},
		{agentModelMooncakePilot, agentModelMooncakePilot, false},
		{"bogus", "", true},
	}
	for _, c := range cases {
		exec, err := newAgentExecutor(c.model, cfg)
		if c.wantErr {
			if err == nil {
				t.Errorf("newAgentExecutor(%q): want error, got %T", c.model, exec)
			}
			continue
		}
		if err != nil {
			t.Errorf("newAgentExecutor(%q): unexpected error %v", c.model, err)
			continue
		}
		if exec.Model() != c.wantModel {
			t.Errorf("newAgentExecutor(%q).Model() = %q, want %q", c.model, exec.Model(), c.wantModel)
		}
	}
}

func TestClaudeExecutorDelegates(t *testing.T) {
	var exec claudeExecutor
	if exec.Model() != agentModelClaudeEdit {
		t.Errorf("Model() = %q, want %q", exec.Model(), agentModelClaudeEdit)
	}
	// Argv must match buildClaudeArgv for the same spec (the executor is a
	// thin wrapper).
	spec := turnSpec{sessionID: "sid", prompt: "do it", systemPrompt: "be good", mcpPath: "/work/mcp.json", resume: false}
	got := exec.Argv(spec)
	if !argvHas(got, "--session-id", "sid") || got[1] != "-p" || got[2] != "do it" {
		t.Errorf("claudeExecutor.Argv didn't delegate to buildClaudeArgv: %v", got)
	}

	// A result line distils into a turnResult; a non-result line doesn't.
	if _, _, res := exec.Translate([]byte(`{"type":"assistant"}`)); res != nil {
		t.Errorf("non-result line yielded a result: %+v", res)
	}
	_, _, res := exec.Translate([]byte(`{"type":"result","subtype":"success","num_turns":3}`))
	if res == nil || res.Subtype != "success" || res.NumTurns != 3 {
		t.Errorf("result line distilled wrong: %+v", res)
	}
}
