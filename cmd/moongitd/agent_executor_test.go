package main

import (
	"strings"
	"testing"

	"github.com/alehatsman/moongit/internal/api"
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
		{agentModelMooncakeAgent, agentModelMooncakeAgent, false},
		{"bogus", "", true},
	}
	for _, c := range cases {
		exec, err := newAgentExecutor(c.model, cfg, false)
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
	// First turn: Argv composes the claude invocation from the raw input —
	// the issue is the -p goal, the session is set (not resumed), and a
	// system prompt is appended.
	turn1 := exec.Argv(turnInput{
		sessionID: "sid",
		owner:     "alice",
		repo:      "repo",
		issue:     api.Issue{Number: 7, Title: "Fix it"},
		firstTurn: true,
		mcpPath:   "/work/mcp.json",
	})
	if !argvHas(turn1, "--session-id", "sid") || turn1[1] != "-p" || !strings.Contains(turn1[2], "Fix it") {
		t.Errorf("first turn didn't compose the claude turn from the issue: %v", turn1)
	}
	if !argvContains(turn1, "--append-system-prompt") {
		t.Errorf("first turn must carry a system prompt: %v", turn1)
	}
	if !argvHas(turn1, "--mcp-config", "/work/mcp.json") {
		t.Errorf("mcp config not wired: %v", turn1)
	}

	// Follow-up turn: resumes the session with the message as the goal, no
	// system prompt (it's already in the resumed session).
	follow := exec.Argv(turnInput{sessionID: "sid", message: "more", resume: true})
	if !argvHas(follow, "--resume", "sid") || follow[2] != "more" || argvContains(follow, "--append-system-prompt") {
		t.Errorf("follow-up turn must resume with the message and no system prompt: %v", follow)
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
