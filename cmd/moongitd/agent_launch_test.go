package main

import (
	"regexp"
	"strings"
	"testing"

	"github.com/alehatsman/moongit/internal/api"
)

var uuidRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func TestAgentSessionIDStableAndValid(t *testing.T) {
	a := agentSessionID(42)
	if !uuidRE.MatchString(a) {
		t.Errorf("session id %q is not UUID-shaped", a)
	}
	if a != agentSessionID(42) {
		t.Error("session id not deterministic for the same run")
	}
	if a == agentSessionID(43) {
		t.Error("different runs must get different session ids")
	}
}

func TestBuildClaudeArgv(t *testing.T) {
	turn1 := buildClaudeArgv("sid", "do it", "be good", false)
	if argvHas(turn1, "--resume", "sid") {
		t.Error("turn 1 must not --resume")
	}
	if !argvHas(turn1, "--session-id", "sid") {
		t.Errorf("turn 1 must set --session-id: %v", turn1)
	}
	if !argvHas(turn1, "--output-format", "stream-json") || !argvContains(turn1, "--verbose") {
		t.Errorf("missing streaming flags: %v", turn1)
	}
	if !argvHas(turn1, "--permission-mode", "bypassPermissions") {
		t.Errorf("missing bypassPermissions: %v", turn1)
	}
	if !argvHas(turn1, "--append-system-prompt", "be good") {
		t.Errorf("missing system prompt: %v", turn1)
	}
	if turn1[1] != "-p" || turn1[2] != "do it" {
		t.Errorf("prompt should be the -p positional: %v", turn1[:3])
	}

	// A follow-up turn resumes the same session instead of setting it.
	follow := buildClaudeArgv("sid", "more", "", true)
	if !argvHas(follow, "--resume", "sid") || argvContains(follow, "--session-id") {
		t.Errorf("follow-up turn must --resume, not --session-id: %v", follow)
	}
	if argvContains(follow, "--append-system-prompt") {
		t.Errorf("empty system prompt should be omitted: %v", follow)
	}
}

func TestComposeTurnPrompt(t *testing.T) {
	withBody := composeTurnPrompt(api.Issue{Number: 7, Title: "Fix it", Body: "the details"})
	if !strings.Contains(withBody, "Fix it") || !strings.Contains(withBody, "the details") {
		t.Errorf("turn prompt missing title/body: %q", withBody)
	}
	noBody := composeTurnPrompt(api.Issue{Number: 7, Title: "Fix it"})
	if !strings.Contains(noBody, "Fix it") {
		t.Errorf("turn prompt missing title: %q", noBody)
	}
}

func TestComposeAgentSystemPrompt(t *testing.T) {
	p := composeAgentSystemPrompt("alice", "repo", api.Issue{Number: 7, Title: "Fix it"})
	for _, want := range []string{"alice/repo", "#7", "Fix it", "/work"} {
		if !strings.Contains(p, want) {
			t.Errorf("system prompt missing %q:\n%s", want, p)
		}
	}
}
