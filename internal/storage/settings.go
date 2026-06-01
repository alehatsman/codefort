package storage

import (
	"database/sql"
	"errors"
)

// Setting keys.
const (
	// SettingAgentClaudeToken holds the operator-set global Claude token
	// (a `claude setup-token` value) injected into agent containers as
	// CLAUDE_CODE_OAUTH_TOKEN. It's a secret — never return it over the API.
	SettingAgentClaudeToken = "agent.claude_oauth_token"

	// SettingAgentExecutionModel is the operator-set default execution model
	// for agent runs that don't pick one at spawn ("claude-edit" |
	// "mooncake-agent"). Unset falls back to DefaultExecutionModel (#110).
	SettingAgentExecutionModel = "agent.execution_model"

	// SettingAgentLLMBaseURL is the operator-set LLM endpoint injected into
	// agent containers as ANTHROPIC_BASE_URL (point claude at a gateway / local
	// model). Not a secret — returned over the API. Overrides the
	// MOONGIT_AGENT_LLM_BASE_URL env so it can be set without a restart.
	SettingAgentLLMBaseURL = "agent.llm_base_url"

	// SettingAgentAnthropicAuthToken is the operator-set bearer token injected
	// as ANTHROPIC_AUTH_TOKEN — the auth for a custom ANTHROPIC_BASE_URL gateway.
	// It's a secret — never return it over the API. When set it takes the agent
	// container's auth slot, ahead of the OAuth/API-key paths.
	SettingAgentAnthropicAuthToken = "agent.anthropic_auth_token"
)

// ValidExecutionModel reports whether m is a known agent execution model.
// Shared by the spawn endpoint and the settings endpoint so both reject
// the same bad values.
func ValidExecutionModel(m string) bool {
	return m == ExecModelClaudeEdit || m == ExecModelMooncakeAgent
}

// GetSetting returns a setting's value, or ErrNotFound when unset.
func GetSetting(db *sql.DB, key string) (string, error) {
	var v string
	err := db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return v, err
}

// SettingValue returns a setting's value, or "" when unset (errors are treated
// as unset). Convenience for callers that just want an effective value.
func SettingValue(db *sql.DB, key string) string {
	v, err := GetSetting(db, key)
	if err != nil {
		return ""
	}
	return v
}

// SetSetting upserts a setting's value.
func SetSetting(db *sql.DB, key, value string) error {
	_, err := db.Exec(`
		INSERT INTO settings(key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = strftime('%s','now')
	`, key, value)
	return err
}

// DeleteSetting removes a setting. Idempotent — removing a missing key is a
// no-op, not an error.
func DeleteSetting(db *sql.DB, key string) error {
	_, err := db.Exec(`DELETE FROM settings WHERE key = ?`, key)
	return err
}
