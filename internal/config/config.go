package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	Addr     string
	DataDir  string
	DBPath   string
	ReposDir string

	// ClaimLease is how long an issue claim stays exclusive before another
	// agent may steal it. A claim older than this is treated as orphaned
	// (the owner crashed or lost context). The owner refreshes the lease by
	// re-claiming (heartbeat). Zero disables expiry — claims hold until
	// explicitly unclaimed. Set via MOONGIT_CLAIM_LEASE (default 60m).
	ClaimLease time.Duration

	// AgentTokenTTL is how long an idle per-agent session token (name
	// "agent#<n>", minted one-per-spawn by the `ce` launcher) is kept before
	// the token reaper revokes it. Measured from last use (or creation, if
	// never used). These accumulate and are never explicitly revoked, so the
	// reaper sweeps idle ones. Zero disables the sweep. Set via
	// MOONGIT_AGENT_TOKEN_TTL (default 168h = 7d).
	AgentTokenTTL time.Duration

	// CIRunTimeout is the hard wall-clock limit for a single CI run. The
	// runner executes the run under a context with this deadline; an
	// overrunning run is killed and marked error. Set via
	// MOONGIT_CI_RUN_TIMEOUT (default 15m). Zero or negative disables the
	// timeout (not recommended — CI runs untrusted repo code).
	CIRunTimeout time.Duration

	// CIPollInterval is how often the CI runner polls for a queued run when
	// idle. Set via MOONGIT_CI_POLL_INTERVAL (default 5s).
	CIPollInterval time.Duration

	// DexURL is the base URL of a dex `serve` daemon (e.g.
	// http://127.0.0.1:8080). Empty disables the Intel tab. DexToken is
	// the bearer token dex was started with (DEX_SERVE_TOKEN); empty when
	// dex runs token-less on loopback.
	DexURL   string
	DexToken string

	// WebDir is the directory holding the built web UI (web/dist). When
	// set, moongitd serves it as a single-page app with history-API
	// fallback. Empty disables web serving (API + git only).
	WebDir string

	// BasicUser/BasicPass gate the web UI and git smart-HTTP behind HTTP
	// Basic auth. Empty BasicUser disables it (open by default). The /api
	// surface is unaffected — it keeps its Bearer-token auth.
	BasicUser string
	BasicPass string
}

// Load reads MOONGIT_* environment variables and resolves the data directory
// to an absolute path. It performs no filesystem side effects — call
// EnsureDirs separately before opening storage.
func Load() (*Config, error) {
	cfg := &Config{
		Addr:    envOr("MOONGIT_ADDR", ":8080"),
		DataDir: envOr("MOONGIT_DATA_DIR", "data"),
	}

	dataDir, err := filepath.Abs(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	cfg.DataDir = dataDir
	cfg.DBPath = envOr("MOONGIT_DB_PATH", filepath.Join(dataDir, "moongit.db"))
	cfg.ReposDir = envOr("MOONGIT_REPOS_DIR", filepath.Join(dataDir, "repos"))
	cfg.DexURL = strings.TrimRight(envOr("MOONGIT_DEX_URL", ""), "/")
	cfg.DexToken = envOr("MOONGIT_DEX_TOKEN", "")
	cfg.WebDir = envOr("MOONGIT_WEB_DIR", "")
	cfg.BasicUser = envOr("MOONGIT_BASIC_USER", "")
	cfg.BasicPass = envOr("MOONGIT_BASIC_PASS", "")

	lease, err := time.ParseDuration(envOr("MOONGIT_CLAIM_LEASE", "60m"))
	if err != nil {
		return nil, fmt.Errorf("MOONGIT_CLAIM_LEASE: %w", err)
	}
	cfg.ClaimLease = lease

	ttl, err := time.ParseDuration(envOr("MOONGIT_AGENT_TOKEN_TTL", "168h"))
	if err != nil {
		return nil, fmt.Errorf("MOONGIT_AGENT_TOKEN_TTL: %w", err)
	}
	cfg.AgentTokenTTL = ttl

	ciTimeout, err := time.ParseDuration(envOr("MOONGIT_CI_RUN_TIMEOUT", "15m"))
	if err != nil {
		return nil, fmt.Errorf("MOONGIT_CI_RUN_TIMEOUT: %w", err)
	}
	cfg.CIRunTimeout = ciTimeout

	ciPoll, err := time.ParseDuration(envOr("MOONGIT_CI_POLL_INTERVAL", "5s"))
	if err != nil {
		return nil, fmt.Errorf("MOONGIT_CI_POLL_INTERVAL: %w", err)
	}
	cfg.CIPollInterval = ciPoll

	return cfg, nil
}

// EnsureDirs creates the data and repos directories if they don't already
// exist. Idempotent.
func (c *Config) EnsureDirs() error {
	if err := os.MkdirAll(c.DataDir, 0o755); err != nil {
		return err
	}
	return os.MkdirAll(c.ReposDir, 0o755)
}

func envOr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}
