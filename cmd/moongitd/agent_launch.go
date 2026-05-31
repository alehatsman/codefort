package main

import (
	"crypto/sha1"
	"fmt"
	"strings"

	"github.com/alehatsman/moongit/internal/api"
)

// agentSessionID derives a stable RFC-4122 UUID for a run's claude session from
// the run id. Deterministic so turn 1 (`--session-id`) and every later turn
// (`--resume`) agree without persisting anything, and so it survives a moongitd
// restart. claude requires `--session-id` to be UUID-shaped, hence the version
// and variant nibbles.
func agentSessionID(runID int64) string {
	h := sha1.Sum([]byte(fmt.Sprintf("moongit-agent-run-%d", runID)))
	var b [16]byte
	copy(b[:], h[:16])
	b[6] = (b[6] & 0x0f) | 0x50 // version 5
	b[8] = (b[8] & 0x3f) | 0x80 // RFC-4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// buildClaudeArgv assembles the headless claude invocation for one turn. The
// prompt is the turn's user message (the issue body on turn 1, a follow-up
// message thereafter). bypassPermissions is the default because the container
// is the sandbox — claude's in-app prompts are redundant against a hard jail,
// and headless -p has no TTY to answer them anyway (see #76). resume picks
// `--resume` over `--session-id` for follow-up turns on the same session.
func buildClaudeArgv(sessionID, prompt, systemPrompt string, resume bool) []string {
	argv := []string{
		"claude", "-p", prompt,
		"--output-format", "stream-json",
		"--verbose", // required for streaming output under --print
		"--permission-mode", "bypassPermissions",
	}
	if resume {
		argv = append(argv, "--resume", sessionID)
	} else {
		argv = append(argv, "--session-id", sessionID)
	}
	if systemPrompt != "" {
		argv = append(argv, "--append-system-prompt", systemPrompt)
	}
	return argv
}

// composeTurnPrompt is the turn-1 user message: the issue's title and body. The
// repository/role framing lives in the system prompt, so this stays the raw
// task.
func composeTurnPrompt(issue api.Issue) string {
	if strings.TrimSpace(issue.Body) == "" {
		return fmt.Sprintf("Issue #%d: %s", issue.Number, issue.Title)
	}
	return fmt.Sprintf("Issue #%d: %s\n\n%s", issue.Number, issue.Title, issue.Body)
}

// composeAgentSystemPrompt builds the moongit-authored system prompt that
// orients the agent: who it is, where it's working, the task, and the safety
// envelope. Credential-dependent workflow (pushing a branch, commenting on the
// issue) and dex grounding are layered in by #77 / dex#6; this is the baseline
// that's correct without them.
func composeAgentSystemPrompt(owner, repo string, issue api.Issue) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are an autonomous coding agent working on the moongit repository %s/%s.\n", owner, repo)
	fmt.Fprintf(&b, "Your task is issue #%d: %q.\n\n", issue.Number, issue.Title)
	b.WriteString("The working directory /work is a clean checkout at the issue's base commit. ")
	b.WriteString("Make focused changes there that address the issue; do not wander beyond its scope.\n")
	b.WriteString("Stay within /work. You run inside an isolated container with no ambient credentials ")
	b.WriteString("and tightly scoped network access — treat anything outside the workspace as off-limits.\n")
	b.WriteString("When you finish, end your turn with a concise summary of what you changed and why.\n")
	b.WriteString("If a decision falls outside your mandate, do not guess: stop and ask it as your final ")
	b.WriteString("message, and a human will answer in the next turn.\n")
	return b.String()
}
