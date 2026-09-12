package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alehatsman/codefort/internal/config"
	"github.com/alehatsman/codefort/internal/storage"
)

// agentMCPConfigName is the MCP config file written into the workspace (so it's
// reachable at /work/<name> inside the container).
const agentMCPConfigName = ".codefort-agent-mcp.json"

// agentTokenName is the deterministic name of a run's ephemeral cf token,
// so it can be revoked on finalize without persisting anything extra.
func agentTokenName(runID int64) string {
	return fmt.Sprintf("agent-run-%d", runID)
}

// agentSettingsOverride carries the operator-set agent config (the Settings
// store, #106) that overrides the server-env config per run — applied without a
// codefortd restart. Empty fields fall back to the env config.
type agentSettingsOverride struct {
	claudeToken        string // -> CLAUDE_CODE_OAUTH_TOKEN
	llmBaseURL         string // -> ANTHROPIC_BASE_URL
	anthropicAuthToken string // -> ANTHROPIC_AUTH_TOKEN (gateway bearer)
}

// agentContainerEnv builds the KEY=VALUE environment injected into an agent
// container at creation — everything per-run and scoped, nothing in the image.
// Auth precedence: an operator-set gateway bearer (ANTHROPIC_AUTH_TOKEN) wins
// and claims the auth slot alone; else the operator-set OAuth token, then the
// OAuth-token env, then the API-key env. ANTHROPIC_BASE_URL is the operator-set
// value when present, else the env. The ephemeral cf token + server URL
// let the in-container git/cf talk to codefortd. Order is stable for
// testability.
func agentContainerEnv(cfg *config.Config, o agentSettingsOverride, codefortToken, serverURL string) []string {
	var env []string
	switch {
	case o.anthropicAuthToken != "":
		env = append(env, "ANTHROPIC_AUTH_TOKEN="+o.anthropicAuthToken)
	case o.claudeToken != "":
		env = append(env, "CLAUDE_CODE_OAUTH_TOKEN="+o.claudeToken)
	case cfg.AgentClaudeOAuthToken != "":
		env = append(env, "CLAUDE_CODE_OAUTH_TOKEN="+cfg.AgentClaudeOAuthToken)
	case cfg.AgentAnthropicAPIKey != "":
		env = append(env, "ANTHROPIC_API_KEY="+cfg.AgentAnthropicAPIKey)
	}
	baseURL := o.llmBaseURL
	if baseURL == "" {
		baseURL = cfg.AgentLLMBaseURL
	}
	if baseURL != "" {
		env = append(env, "ANTHROPIC_BASE_URL="+baseURL)
	}
	if codefortToken != "" {
		env = append(env, "CODEFORT_TOKEN="+codefortToken)
	}
	if serverURL != "" {
		env = append(env, "CODEFORT_SERVER="+serverURL)
	}
	return env
}

// agentServerURL is how the in-container agent reaches codefortd: the configured
// override, else host.docker.internal at codefortd's listen port.
func agentServerURL(cfg *config.Config) string {
	if cfg.AgentServerURL != "" {
		return cfg.AgentServerURL
	}
	port := cfg.Addr
	if i := strings.LastIndex(port, ":"); i >= 0 {
		port = port[i+1:]
	}
	if port == "" {
		port = "8080"
	}
	return "http://host.docker.internal:" + port
}

// writeAgentMCPConfig writes the claude --mcp-config file into the workspace,
// registering the stdio MCP servers the agent gets (reachable at /work and run
// under --strict-mcp-config, so this file is the agent's whole MCP surface):
//
//   - cf: the cf issue/review/pipeline/agent toolset (`cf mcp`, #158).
//     Always registered — the per-run CODEFORT_TOKEN + CODEFORT_SERVER ride in the
//     container env (agentContainerEnv), so the shim resolves its target and
//     identity without anything in this file. The run's tool profile (#184) is
//     passed as `--profile <p>`, so the shim only registers the tools that
//     profile permits (shim-side enforcement, robust headless — #110).
//
// cf is currently the only server; the map shape is kept because the config
// format is a map and a second server would slot in without restructuring.
//
// The file therefore always exists for an agent run and carries no secret. It
// returns the in-container path.
func writeAgentMCPConfig(hostWorkDir, toolProfile string) (containerPath string, err error) {
	if toolProfile == "" {
		toolProfile = storage.DefaultToolProfile
	}
	servers := map[string]any{
		"cf": map[string]any{
			"command": "cf",
			"args":    []string{"mcp", "--profile", toolProfile},
		},
	}
	b, err := json.MarshalIndent(map[string]any{"mcpServers": servers}, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(hostWorkDir, agentMCPConfigName), b, 0o644); err != nil {
		return "", err
	}
	return "/work/" + agentMCPConfigName, nil
}
