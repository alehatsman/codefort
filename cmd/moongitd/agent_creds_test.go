package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alehatsman/moongit/internal/config"
)

func TestAgentContainerEnvOAuthAndDex(t *testing.T) {
	cfg := &config.Config{
		AgentClaudeOAuthToken: "oauth",
		AgentLLMBaseURL:       "http://llm.local",
		DexURL:                "http://dex.local",
		DexToken:              "dt",
		DexProject:            "p",
	}
	env := agentContainerEnv(cfg, "mgt_tok", "http://host.docker.internal:8080")
	want := []string{
		"CLAUDE_CODE_OAUTH_TOKEN=oauth",
		"ANTHROPIC_BASE_URL=http://llm.local",
		"MOONGIT_TOKEN=mgt_tok",
		"MOONGIT_SERVER=http://host.docker.internal:8080",
		"DEX_REMOTE_URL=http://dex.local",
		"DEX_SERVE_TOKEN=dt",
		"DEX_PROJECT=p",
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

func TestAgentContainerEnvAPIKeyFallbackNoDex(t *testing.T) {
	cfg := &config.Config{AgentAnthropicAPIKey: "sk-xyz"} // no OAuth, no dex
	env := agentContainerEnv(cfg, "tok", "url")
	if !contains(env, "ANTHROPIC_API_KEY=sk-xyz") {
		t.Errorf("API-key fallback missing: %v", env)
	}
	for _, e := range env {
		if strings.HasPrefix(e, "CLAUDE_CODE_OAUTH_TOKEN=") || strings.HasPrefix(e, "DEX_") {
			t.Errorf("unexpected env %q (no OAuth, no dex): %v", e, env)
		}
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

func TestWriteDexMCPConfig(t *testing.T) {
	dir := t.TempDir()

	// No dex configured -> no file, ok=false.
	if p, ok, err := writeDexMCPConfig(dir, &config.Config{}); err != nil || ok || p != "" {
		t.Fatalf("no-dex = (%q,%v,%v), want (\"\",false,nil)", p, ok, err)
	}

	// Dex configured -> writes a valid MCP config naming the dex shim.
	p, ok, err := writeDexMCPConfig(dir, &config.Config{DexURL: "http://dex.local"})
	if err != nil || !ok {
		t.Fatalf("writeDexMCPConfig: ok=%v err=%v", ok, err)
	}
	if p != "/work/"+dexMCPConfigName {
		t.Errorf("container path = %q, want /work/%s", p, dexMCPConfigName)
	}
	raw, err := os.ReadFile(filepath.Join(dir, dexMCPConfigName))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var conf struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &conf); err != nil {
		t.Fatalf("config not valid JSON: %v", err)
	}
	dex, present := conf.MCPServers["dex"]
	if !present || dex.Command != "dex" || !sliceContains(dex.Args, "http://dex.local") {
		t.Errorf("dex server config wrong: %+v", conf.MCPServers)
	}
	// The bearer is NOT in the file — it rides in the container env.
	if strings.Contains(string(raw), "DEX_SERVE_TOKEN") {
		t.Errorf("MCP config file should carry no secret:\n%s", raw)
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
