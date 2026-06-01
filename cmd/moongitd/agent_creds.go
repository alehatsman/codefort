package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alehatsman/moongit/internal/config"
)

// dexMCPConfigName is the MCP config file written into the workspace (so it's
// reachable at /work/<name> inside the container).
const dexMCPConfigName = ".moongit-agent-mcp.json"

// agentTokenName is the deterministic name of a run's ephemeral moongit token,
// so it can be revoked on finalize without persisting anything extra.
func agentTokenName(runID int64) string {
	return fmt.Sprintf("agent-run-%d", runID)
}

// agentSettingsOverride carries the operator-set agent config (the Settings
// store, #106) that overrides the server-env config per run — applied without a
// moongitd restart. Empty fields fall back to the env config.
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
// value when present, else the env. The ephemeral moongit token + server URL
// let the in-container git/mgit talk to moongitd; the dex bearer/endpoint/
// project wire the hot index when configured. Order is stable for testability.
func agentContainerEnv(cfg *config.Config, o agentSettingsOverride, moongitToken, serverURL string) []string {
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
	if moongitToken != "" {
		env = append(env, "MOONGIT_TOKEN="+moongitToken)
	}
	if serverURL != "" {
		env = append(env, "MOONGIT_SERVER="+serverURL)
	}
	if cfg.DexURL != "" {
		env = append(env, "DEX_REMOTE_URL="+cfg.DexURL)
		if cfg.DexToken != "" {
			env = append(env, "DEX_SERVE_TOKEN="+cfg.DexToken)
		}
		if cfg.DexProject != "" {
			env = append(env, "DEX_PROJECT="+cfg.DexProject)
		}
	}
	return env
}

// agentServerURL is how the in-container agent reaches moongitd: the configured
// override, else host.docker.internal at moongitd's listen port.
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

// writeDexMCPConfig writes a claude --mcp-config file into the workspace,
// pointing at the dex shim (option A: a stdio `dex mcp --remote` on PATH in the
// agent image, per dex#6). It returns the in-container path, or ok=false when
// dex isn't configured. The bearer/project ride in the container env, so the
// file carries no secret.
func writeDexMCPConfig(hostWorkDir string, cfg *config.Config) (containerPath string, ok bool, err error) {
	if cfg.DexURL == "" {
		return "", false, nil
	}
	conf := map[string]any{
		"mcpServers": map[string]any{
			"dex": map[string]any{
				"command": "dex",
				"args":    []string{"mcp", "--remote", cfg.DexURL},
			},
		},
	}
	b, err := json.MarshalIndent(conf, "", "  ")
	if err != nil {
		return "", false, err
	}
	if err := os.WriteFile(filepath.Join(hostWorkDir, dexMCPConfigName), b, 0o644); err != nil {
		return "", false, err
	}
	return "/work/" + dexMCPConfigName, true, nil
}
