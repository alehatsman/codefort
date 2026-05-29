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

	// DexURL is the base URL of a dex `serve` daemon (e.g.
	// http://127.0.0.1:8080). Empty disables the Intel tab. DexToken is
	// the bearer token dex was started with (DEX_SERVE_TOKEN); empty when
	// dex runs token-less on loopback.
	DexURL   string
	DexToken string
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

	lease, err := time.ParseDuration(envOr("MOONGIT_CLAIM_LEASE", "60m"))
	if err != nil {
		return nil, fmt.Errorf("MOONGIT_CLAIM_LEASE: %w", err)
	}
	cfg.ClaimLease = lease

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
