package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alehatsman/codefort/internal/config"
	"github.com/alehatsman/codefort/internal/storage"
)

func TestAgentContainerEnvOAuth(t *testing.T) {
	cfg := &config.Config{
		AgentClaudeOAuthToken: "oauth",
		AgentLLMBaseURL:       "http://llm.local",
	}
	env := agentContainerEnv(cfg, agentSettingsOverride{}, "mgt_tok", "http://host.docker.internal:8080")
	want := []string{
		"CLAUDE_CODE_OAUTH_TOKEN=oauth",
		"ANTHROPIC_BASE_URL=http://llm.local",
		"MOONGIT_TOKEN=mgt_tok",
		"MOONGIT_SERVER=http://host.docker.internal:8080",
	}
	for _, w := range want {
		if !contains(env, w) {
			t.Errorf("env missing %q; got %v", w, env)
		}
	}
	// No API key when the OAuth token is present.
	for _, e := range env {
		if strings.HasPrefix(e, "ANTHROPIC_API_KEY=") {
			t.Errorf("ANTHROPIC_API_KEY should not be set alongside OAuth: %v", env)
		}
	}
}

func TestAgentContainerEnvAPIKeyFallback(t *testing.T) {
	cfg := &config.Config{AgentAnthropicAPIKey: "sk-xyz"} // no OAuth
	env := agentContainerEnv(cfg, agentSettingsOverride{}, "tok", "url")
	if !contains(env, "ANTHROPIC_API_KEY=sk-xyz") {
		t.Errorf("API-key fallback missing: %v", env)
	}
	for _, e := range env {
		if strings.HasPrefix(e, "CLAUDE_CODE_OAUTH_TOKEN=") {
			t.Errorf("unexpected env %q (no OAuth configured): %v", e, env)
		}
	}
}

// An operator-set token (Settings, #106) wins over the env OAuth/API-key.
func TestAgentContainerEnvOverrideWins(t *testing.T) {
	cfg := &config.Config{AgentClaudeOAuthToken: "from-env", AgentAnthropicAPIKey: "sk-env"}
	env := agentContainerEnv(cfg, agentSettingsOverride{claudeToken: "from-settings"}, "tok", "url")
	if !contains(env, "CLAUDE_CODE_OAUTH_TOKEN=from-settings") {
		t.Errorf("override not used: %v", env)
	}
	for _, e := range env {
		if e == "CLAUDE_CODE_OAUTH_TOKEN=from-env" || strings.HasPrefix(e, "ANTHROPIC_API_KEY=") {
			t.Errorf("env creds leaked past the override: %v", env)
		}
	}
}

// An operator-set base URL (Settings) overrides the env, and the gateway auth
// token claims the auth slot alone — no OAuth/API-key alongside it.
func TestAgentContainerEnvGatewayOverride(t *testing.T) {
	cfg := &config.Config{
		AgentClaudeOAuthToken: "from-env",
		AgentLLMBaseURL:       "http://env.local",
	}
	env := agentContainerEnv(cfg, agentSettingsOverride{
		llmBaseURL:         "http://gateway.local",
		anthropicAuthToken: "sk-gw",
	}, "tok", "url")
	if !contains(env, "ANTHROPIC_BASE_URL=http://gateway.local") {
		t.Errorf("base URL override not used: %v", env)
	}
	if !contains(env, "ANTHROPIC_AUTH_TOKEN=sk-gw") {
		t.Errorf("gateway auth token missing: %v", env)
	}
	if contains(env, "ANTHROPIC_BASE_URL=http://env.local") {
		t.Errorf("env base URL leaked past the override: %v", env)
	}
	for _, e := range env {
		if strings.HasPrefix(e, "CLAUDE_CODE_OAUTH_TOKEN=") || strings.HasPrefix(e, "ANTHROPIC_API_KEY=") {
			t.Errorf("auth slot should be the gateway token alone: %v", env)
		}
	}
}

// With no Settings base URL the env value still flows through.
func TestAgentContainerEnvBaseURLEnvFallback(t *testing.T) {
	cfg := &config.Config{AgentLLMBaseURL: "http://env.local"}
	env := agentContainerEnv(cfg, agentSettingsOverride{}, "tok", "url")
	if !contains(env, "ANTHROPIC_BASE_URL=http://env.local") {
		t.Errorf("env base URL fallback missing: %v", env)
	}
}

func TestAgentServerURL(t *testing.T) {
	if got := agentServerURL(&config.Config{AgentServerURL: "http://x"}); got != "http://x" {
		t.Errorf("override = %q, want http://x", got)
	}
	if got := agentServerURL(&config.Config{Addr: ":9090"}); got != "http://host.docker.internal:9090" {
		t.Errorf("derived = %q, want host.docker.internal:9090", got)
	}
}

func TestWriteAgentMCPConfig(t *testing.T) {
	type serverConf struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	readConf := func(t *testing.T, dir string) map[string]serverConf {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(dir, agentMCPConfigName))
		if err != nil {
			t.Fatalf("read config: %v", err)
		}
		var conf struct {
			MCPServers map[string]serverConf `json:"mcpServers"`
		}
		if err := json.Unmarshal(raw, &conf); err != nil {
			t.Fatalf("config not valid JSON: %v", err)
		}
		// No secret rides in the file — bearer/token are env-only.
		if strings.Contains(string(raw), "MOONGIT_TOKEN") {
			t.Errorf("MCP config file should carry no secret:\n%s", raw)
		}
		return conf.MCPServers
	}

	// An empty profile defaults to "full" in the shim argv.
	dir := t.TempDir()
	p, err := writeAgentMCPConfig(dir, "")
	if err != nil {
		t.Fatalf("writeAgentMCPConfig: %v", err)
	}
	if p != "/work/"+agentMCPConfigName {
		t.Errorf("container path = %q, want /work/%s", p, agentMCPConfigName)
	}
	servers := readConf(t, dir)
	if len(servers) != 1 {
		t.Errorf("mgit should be the only MCP server: %+v", servers)
	}
	mgit, ok := servers["mgit"]
	if !ok || mgit.Command != "mgit" || !sliceContains(mgit.Args, "mcp") {
		t.Errorf("mgit server config wrong: %+v", servers)
	}
	if !sliceContains(mgit.Args, "--profile") || !sliceContains(mgit.Args, storage.ToolProfileFull) {
		t.Errorf("mgit args should carry the default profile: %+v", mgit.Args)
	}

	// An explicit profile rides through to the shim argv.
	dir = t.TempDir()
	if _, err := writeAgentMCPConfig(dir, storage.ToolProfileReview); err != nil {
		t.Fatalf("writeAgentMCPConfig (review profile): %v", err)
	}
	mgit, ok = readConf(t, dir)["mgit"]
	if !ok {
		t.Fatal("mgit server missing")
	}
	if !sliceContains(mgit.Args, "--profile") || !sliceContains(mgit.Args, storage.ToolProfileReview) {
		t.Errorf("mgit args should carry the review profile: %+v", mgit.Args)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func sliceContains(ss []string, want string) bool { return contains(ss, want) }
