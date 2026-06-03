package main

import (
	"strings"
	"testing"

	"github.com/alehatsman/moongit/internal/api"
)

func TestComposeVerifyTurnPrompt(t *testing.T) {
	content := "---\nid: ssh\ncovers: [\"internal/ssh/**\"]\n---\n# SSH\n## Behavior\nWHEN x THEN y.\n"
	p := composeVerifyTurnPrompt("specs/ssh.md", content)

	if !strings.Contains(p, "specs/ssh.md") {
		t.Error("prompt omits the spec path")
	}
	// The spec content is inlined for the agent.
	if !strings.Contains(p, "WHEN x THEN y.") || !strings.Contains(p, "covers:") {
		t.Error("prompt doesn't inline the spec content")
	}
	// It asks for the structured JSON with the taxonomy + line numbers.
	for _, want := range []string{"```json", "alignment", "markers", "aligned|drifted|unverifiable|unspecced", "1-based"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestComposeVerifySystemPrompt(t *testing.T) {
	sys := composeVerifySystemPrompt("alice", "proj")

	if !strings.Contains(sys, "alice/proj") {
		t.Error("system prompt omits repo identity")
	}
	// The anti-false-confidence fence: agreement only, never correctness.
	low := strings.ToLower(sys)
	if !strings.Contains(low, "match") || !strings.Contains(low, "never judge") {
		t.Errorf("system prompt must fence off judging correctness:\n%s", sys)
	}
	// Read-only posture.
	if !strings.Contains(low, "read-only") || !strings.Contains(low, "do not edit") {
		t.Errorf("system prompt must state read-only posture:\n%s", sys)
	}
}

// A spec-verify turn input drives the verify prompts, not the issue ones.
func TestTurnInputVerifyGoal(t *testing.T) {
	in := turnInput{verify: true, specPath: "specs/x.md", specContent: "# X\n", firstTurn: true}
	if !strings.Contains(in.goal(), "Verify the spec at specs/x.md") {
		t.Errorf("verify goal = %q", in.goal())
	}
	// A normal issue turn is unaffected.
	issueIn := turnInput{issue: api.Issue{Number: 7, Title: "t"}, firstTurn: true}
	if !strings.Contains(issueIn.goal(), "Issue #7") {
		t.Errorf("issue goal = %q", issueIn.goal())
	}
}
