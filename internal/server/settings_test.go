package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/alehatsman/codefort/internal/api"
	"github.com/alehatsman/codefort/internal/config"
	"github.com/alehatsman/codefort/internal/storage"
)

func newSettingsServer(t *testing.T) *Server {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return &Server{cfg: &config.Config{}, db: db, rdb: db, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func agentSettings(t *testing.T, s *Server) api.AgentSettings {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/settings/agent", nil)
	req = req.WithContext(context.WithValue(req.Context(), tokenCtxKey{}, api.Token{Name: "op"}))
	rr := httptest.NewRecorder()
	s.handleGetAgentSettings(rr, req)
	var out api.AgentSettings
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func putAgentSettings(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/settings/agent", bytes.NewReader([]byte(body)))
	req = req.WithContext(context.WithValue(req.Context(), tokenCtxKey{}, api.Token{Name: "op"}))
	rr := httptest.NewRecorder()
	s.handleUpdateAgentSettings(rr, req)
	return rr
}

// Setting, reporting (write-only), and clearing the global agent Claude token.
func TestAgentSettingsLifecycle(t *testing.T) {
	s := newSettingsServer(t)

	if agentSettings(t, s).ClaudeTokenSet {
		t.Fatal("token should start unset")
	}

	// Set it.
	if rr := putAgentSettings(t, s, `{"claude_oauth_token":"sk-ant-oat01-xyz"}`); rr.Code != http.StatusOK {
		t.Fatalf("set code = %d, body=%s", rr.Code, rr.Body.String())
	}
	if !agentSettings(t, s).ClaudeTokenSet {
		t.Error("token should be set")
	}
	// The value is stored (runner reads it) but never returned by the API.
	if got := storage.SettingValue(s.db, storage.SettingAgentClaudeToken); got != "sk-ant-oat01-xyz" {
		t.Errorf("stored token = %q, want the set value", got)
	}
	if body := agentSettings(t, s); body.ClaudeTokenSet {
		// AgentSettings has no token field at all — assert the JSON carries no secret.
		raw, _ := json.Marshal(body)
		if bytes.Contains(raw, []byte("sk-ant-oat01")) {
			t.Errorf("GET leaked the token: %s", raw)
		}
	}

	// nil pointer leaves it unchanged.
	if rr := putAgentSettings(t, s, `{}`); rr.Code != http.StatusOK {
		t.Fatalf("noop code = %d", rr.Code)
	}
	if !agentSettings(t, s).ClaudeTokenSet {
		t.Error("token should still be set after a no-op update")
	}

	// Empty string clears it.
	if rr := putAgentSettings(t, s, `{"claude_oauth_token":""}`); rr.Code != http.StatusOK {
		t.Fatalf("clear code = %d", rr.Code)
	}
	if agentSettings(t, s).ClaudeTokenSet {
		t.Error("token should be cleared")
	}
}

// The env fallback (MOONGIT_AGENT_CLAUDE_OAUTH_TOKEN / _ANTHROPIC_API_KEY) is
// reported independently of the DB token, so the UI can tell "authenticating
// via env" from "no auth at all".
func TestAgentSettingsEnvFallbackReported(t *testing.T) {
	s := newSettingsServer(t)
	if agentSettings(t, s).EnvFallbackSet {
		t.Fatal("no env credential configured — fallback should be false")
	}

	s.cfg.AgentClaudeOAuthToken = "from-env"
	if !agentSettings(t, s).EnvFallbackSet {
		t.Error("OAuth env token present — fallback should be true")
	}

	s.cfg.AgentClaudeOAuthToken = ""
	s.cfg.AgentAnthropicAPIKey = "sk-env"
	if !agentSettings(t, s).EnvFallbackSet {
		t.Error("API-key env present — fallback should be true")
	}
}

// The LLM base URL is operator-set and NOT a secret — it's returned as-is; the
// gateway auth token is write-only, reported only as set/unset like the Claude
// token.
func TestAgentSettingsGatewayFields(t *testing.T) {
	s := newSettingsServer(t)

	got := agentSettings(t, s)
	if got.LLMBaseURL != "" || got.AuthTokenSet {
		t.Fatalf("gateway fields should start empty/unset: %+v", got)
	}

	if rr := putAgentSettings(t, s, `{"llm_base_url":"http://gw.local","anthropic_auth_token":"sk-gw"}`); rr.Code != http.StatusOK {
		t.Fatalf("set code = %d, body=%s", rr.Code, rr.Body.String())
	}
	got = agentSettings(t, s)
	if got.LLMBaseURL != "http://gw.local" {
		t.Errorf("base URL = %q, want it returned as-is", got.LLMBaseURL)
	}
	if !got.AuthTokenSet {
		t.Error("auth token should report set")
	}
	// The auth token is stored (runner reads it) but never returned.
	if v := storage.SettingValue(s.db, storage.SettingAgentAnthropicAuthToken); v != "sk-gw" {
		t.Errorf("stored auth token = %q, want the set value", v)
	}
	raw, _ := json.Marshal(got)
	if bytes.Contains(raw, []byte("sk-gw")) {
		t.Errorf("GET leaked the auth token: %s", raw)
	}

	// Empty string clears each field.
	if rr := putAgentSettings(t, s, `{"llm_base_url":"","anthropic_auth_token":""}`); rr.Code != http.StatusOK {
		t.Fatalf("clear code = %d", rr.Code)
	}
	got = agentSettings(t, s)
	if got.LLMBaseURL != "" || got.AuthTokenSet {
		t.Errorf("gateway fields should be cleared: %+v", got)
	}
}
