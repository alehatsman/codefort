package main

import (
	"crypto/sha1" //nolint:gosec // G505: SHA-1 here is RFC-4122 UUIDv5 derivation, not a security primitive.
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
	h := sha1.Sum([]byte(fmt.Sprintf("moongit-agent-run-%d", runID))) //nolint:gosec // G401: UUIDv5 is defined to use SHA-1; not a security hash.
	var b [16]byte
	copy(b[:], h[:16])
	b[6] = (b[6] & 0x0f) | 0x50 // version 5
	b[8] = (b[8] & 0x3f) | 0x80 // RFC-4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// buildClaudeArgv assembles the headless claude invocation for one turn. The
// prompt is the turn's user message (the issue body on turn 1, a follow-up
// message thereafter). resume picks `--resume` over `--session-id` for
// follow-up turns on the same session. mcpConfigPath, when set, attaches the
// agent MCP servers (mgit, plus dex when configured), restricting claude to
// only the servers in that file (--strict-mcp-config).
//
// Permissions: --permission-mode bypassPermissions reliably auto-approves the
// file/search tools (Edit/Write/Read/Glob/Grep) — which is what lets the agent
// resolve a code issue. Execution tools (Bash) are NOT reliably unlockable
// headlessly under subscription auth — neither this flag,
// --dangerously-skip-permissions, --allowedTools, nor a settings.json allow
// survives the session-id/system-prompt flags the agent needs (a managed/usage
// policy re-gates them). So the agent edits files; running commands (git,
// tests, mgit) belongs to a moongit/mooncake-controlled executor — see #110.
func buildClaudeArgv(sessionID, prompt, systemPrompt, mcpConfigPath string, resume bool) []string {
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
	if mcpConfigPath != "" {
		argv = append(argv, "--mcp-config", mcpConfigPath, "--strict-mcp-config")
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
	b.WriteString("Do your work by editing files in /work directly (read/edit/write). Running shell ")
	b.WriteString("commands isn't available in this session, so don't rely on git, tests, or other ")
	b.WriteString("CLIs — if a command needs running, describe it in your summary and it'll be handled ")
	b.WriteString("by moongit. moongit snapshots your file changes into a branch and posts the summary ")
	b.WriteString("for you when the run is finished.\n")
	b.WriteString("When you finish, end your turn with a concise summary of what you changed and why.\n")
	b.WriteString("If a decision falls outside your mandate, do not guess: stop and ask it as your final ")
	b.WriteString("message, and a human will answer in the next turn.\n")
	return b.String()
}

// composeVerifyTurnPrompt is the spec-verify task: hand the agent the spec and
// ask for a per-line drift classification as structured JSON. The spec content
// is inlined so the agent doesn't have to find it; covers[] tells it which code
// to read (via dex + the checkout).
func composeVerifyTurnPrompt(specPath, content string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Verify the spec at %s against the code it governs.\n\n", specPath)
	b.WriteString("The spec's full content (frontmatter + body):\n\n```markdown\n")
	b.WriteString(content)
	if !strings.HasSuffix(content, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("```\n\n")
	b.WriteString("Read the code under the spec's `covers` globs (use the dex tools to search/summarize, ")
	b.WriteString("and read files under /work). For each behavior and checklist line, decide whether the ")
	b.WriteString("code bears it out.\n\n")
	b.WriteString("Output ONLY a single fenced ```json block (no prose outside it) of this shape:\n")
	b.WriteString("{\n")
	b.WriteString(`  "alignment": 0.0,` + "  // fraction in [0,1] of verifiable lines that are aligned\n")
	b.WriteString(`  "markers": [ {"line": <int>, "text": "<line>", "marker": "aligned|drifted|unverifiable|unspecced", "note": "<evidence>"} ],` + "\n")
	b.WriteString(`  "conflicts": ["<short description of each drifted point>"],` + "\n")
	b.WriteString(`  "notes": "<optional overall remarks>"` + "\n")
	b.WriteString("}\n\n")
	b.WriteString("Use the line numbers from the spec content above (1-based, frontmatter included).\n")
	return b.String()
}

// composeVerifySystemPrompt orients the agent for a read-only verify pass and,
// critically, fences off the anti-pattern: it reports spec↔code AGREEMENT, never
// whether the spec is *correct* (quality is judged separately). Marker meanings
// follow the spec-kit-sync taxonomy.
func composeVerifySystemPrompt(owner, repo string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are a spec-verification agent for the moongit repository %s/%s.\n", owner, repo)
	b.WriteString("Your job is to check whether the code MATCHES the spec — not whether the spec is a good ")
	b.WriteString("or correct spec. Never judge the spec's correctness, design, or completeness; only report ")
	b.WriteString("agreement between what the spec says and what the code does.\n\n")
	b.WriteString("This is READ-ONLY. Do not edit, create, or delete any file; do not run mutating commands. ")
	b.WriteString("Read /work and use the dex tools to find and summarize the governed code.\n\n")
	b.WriteString("Marker meanings: aligned = code bears out the line; drifted = code contradicts it; ")
	b.WriteString("unverifiable = can't tell from the code; unspecced = code behavior the spec doesn't cover.\n")
	b.WriteString("Base every verdict on evidence you actually found (cite file:line in the note). If you ")
	b.WriteString("cannot find the governed code, mark lines unverifiable rather than guessing.\n")
	b.WriteString("End your turn with the single JSON block and nothing else.\n")
	return b.String()
}
