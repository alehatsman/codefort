package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr     string
	DataDir  string
	DBPath   string
	ReposDir string

	// HostDataDir is the host-side path that corresponds to DataDir when
	// moongitd runs inside a Docker container. The CI and agent runners
	// bind-mount workspace directories into sibling containers via the host
	// Docker daemon, which resolves bind-mount sources on the HOST filesystem.
	// When moongitd is containerised, DataDir is the in-container path (e.g.
	// /data), while the host has the same tree at a different path (e.g.
	// /home/user/.local/share/moongit). Set CODEFORT_HOST_DATA_DIR to that
	// host path; leave it empty (or equal to DataDir) for non-containerised
	// deployments. Set via CODEFORT_HOST_DATA_DIR.
	HostDataDir string

	// ClaimLease is how long an issue claim stays exclusive before another
	// agent may steal it. A claim older than this is treated as orphaned
	// (the owner crashed or lost context). The owner refreshes the lease by
	// re-claiming (heartbeat). Zero disables expiry — claims hold until
	// explicitly unclaimed. Set via CODEFORT_CLAIM_LEASE (default 60m).
	ClaimLease time.Duration

	// AgentTokenTTL is how long an idle per-agent session token (name
	// "agent#<n>", minted one-per-spawn by the `ce` launcher) is kept before
	// the token reaper revokes it. Measured from last use (or creation, if
	// never used). These accumulate and are never explicitly revoked, so the
	// reaper sweeps idle ones. Zero disables the sweep. Set via
	// CODEFORT_AGENT_TOKEN_TTL (default 168h = 7d).
	AgentTokenTTL time.Duration

	// CIRunTimeout is the hard wall-clock limit for a single CI run. The
	// runner executes the run under a context with this deadline; an
	// overrunning run is killed and marked error. Set via
	// CODEFORT_CI_RUN_TIMEOUT (default 15m). Zero or negative disables the
	// timeout (not recommended — CI runs untrusted repo code).
	CIRunTimeout time.Duration

	// CIPollInterval is how often the CI runner polls for a queued run when
	// idle. Set via CODEFORT_CI_POLL_INTERVAL (default 5s). As with the
	// concurrency values, the runner applies the floor — see minCIPollInterval.
	CIPollInterval time.Duration

	// CIJobConcurrency caps how many of a run's jobs execute at once: the
	// runner schedules jobs in dependency waves and runs every ready job (all
	// needs satisfied) concurrently up to this many. Set via
	// CODEFORT_CI_JOB_CONCURRENCY (default 4). Load stores the value as given;
	// the runner is what treats < 1 as 1, so a nonsense value survives Load.
	CIJobConcurrency int

	// MaxConcurrency caps how many runs execute at once across one shared budget
	// that CI and agent runs both draw from. Set via CODEFORT_MAX_CONCURRENCY
	// (default runtime.NumCPU()). As with CIJobConcurrency, the < 1 floor is
	// applied by the runner, not here. A CI run still
	// bounds its own jobs by CIJobConcurrency, so this caps concurrent *runs*,
	// not strictly job containers. Replaces the former independent CI/agent caps.
	MaxConcurrency int

	// CISecret gates the loopback /internal/ci/events endpoint that the
	// post-receive hook calls. Set via CODEFORT_CI_SECRET; empty means the
	// server generates a fresh per-process secret at startup (sufficient,
	// since it's injected into the hook env at push time and never persisted).
	CISecret string

	// CIIsolation selects how the runner executes a job's steps. "docker"
	// (default) runs each job in a throwaway container so repo-authored
	// commands never touch the host; "none" runs them on the host as the
	// moongitd user (the legacy path — RCE by design, use only when you trust
	// every CI-enabled repo). Set via CODEFORT_CI_ISOLATION.
	CIIsolation string

	// CIDefaultImage is the container image a job runs in when its codefort.yml
	// doesn't set `image:`. It must be glibc-based and carry `provision`, `git`,
	// and `curl` on PATH — see ci/Dockerfile. Only used when CIIsolation="docker".
	// Set via CODEFORT_CI_DEFAULT_IMAGE.
	CIDefaultImage string

	// AgentReserved is how many of MaxConcurrency's slots only agent-family runs
	// may take, so a CI backlog can never lock out an agent spawn (the inverse
	// starves too — see the runner's fair, non-blocking drain). Set via
	// CODEFORT_AGENT_RESERVED (default 2). The clamp to [0, MaxConcurrency] is
	// the work budget's, not Load's — see the startup warning in the runner for
	// why a value at or above MaxConcurrency is worth noticing.
	AgentReserved int

	// AgentRunTimeout is the whole-session lifetime cap for an agent run: a run
	// parked in awaiting_input is reaped (container torn down, run finalized)
	// once it's older than this, so an abandoned session can't hold a container
	// forever. Set via CODEFORT_AGENT_RUN_TIMEOUT (default 60m). Zero or negative
	// disables the reaper.
	AgentRunTimeout time.Duration

	// AgentTurnTimeout is the per-turn wall-clock limit: one claude invocation
	// (turn 1 or a follow-up) runs under this deadline; an overrunning turn is
	// killed and the turn errored. Set via CODEFORT_AGENT_TURN_TIMEOUT (default
	// 15m). Zero or negative disables the per-turn deadline.
	AgentTurnTimeout time.Duration

	// AgentDefaultImage is the container image an agent run executes in: the CI
	// base image plus the Claude CLI (#75). Set via
	// CODEFORT_AGENT_DEFAULT_IMAGE. Only used when CIIsolation="docker".
	AgentDefaultImage string

	// Agent credentials, injected per-run into the container env — never baked
	// into the image (#77). AgentClaudeOAuthToken is the subscription token from
	// `claude setup-token` (CLAUDE_CODE_OAUTH_TOKEN); AgentAnthropicAPIKey is the
	// alternate API-key path (ANTHROPIC_API_KEY); exactly one is needed for the
	// agent to authenticate headlessly. AgentLLMBaseURL optionally overrides the
	// LLM endpoint (ANTHROPIC_BASE_URL — Anthropic now, a local GPU model later).
	// Set via CODEFORT_AGENT_CLAUDE_OAUTH_TOKEN / _ANTHROPIC_API_KEY / _LLM_BASE_URL.
	AgentClaudeOAuthToken string
	AgentAnthropicAPIKey  string
	AgentLLMBaseURL       string

	// AgentServerURL is how the in-container agent reaches this moongitd (for
	// mgit / git over the host gateway). Empty defaults to
	// http://host.docker.internal:<port-of-Addr>. Set via CODEFORT_AGENT_SERVER_URL.
	AgentServerURL string

	// CIRetainRuns caps how many of a repo's most recent CI runs are kept: a
	// periodic reaper prunes terminal runs beyond this many (and their on-disk
	// event logs), keeping disk + DB bounded. queued/running runs are never
	// pruned. Set via CODEFORT_CI_RETAIN_RUNS (default 50); zero or negative
	// disables retention.
	CIRetainRuns int

	// EventRetain caps how many of the most recent outbound feed events (#73)
	// are kept: a periodic reaper prunes older rows, bounding the events table.
	// Set via CODEFORT_EVENT_RETAIN (default 10000); zero or negative disables
	// retention.
	EventRetain int

	// WebDir is the directory holding the built web UI (web/dist). When
	// set, moongitd serves it as a single-page app with history-API
	// fallback. Empty disables web serving (API + git only).
	WebDir string

	// BasicUser/BasicPass gate the web UI and git smart-HTTP behind HTTP
	// Basic auth. Empty BasicUser disables it (open by default). The /api
	// surface is unaffected — it keeps its Bearer-token auth.
	BasicUser string
	BasicPass string

	// SSHAddr is the listen address for the opt-in git SSH transport (e.g.
	// ":2222"). Empty (default) disables SSH entirely, keeping moongitd a
	// single HTTP port. When set, moongitd serves git over SSH with publickey
	// auth against registered keys. Set via CODEFORT_SSH_ADDR.
	SSHAddr string

	// SSHHostKey is the path to the persisted SSH host private key. It's
	// generated (ed25519, 0600) on first use if absent so the host identity is
	// stable across restarts. Set via CODEFORT_SSH_HOST_KEY (default
	// "$CODEFORT_DATA_DIR/ssh_host_ed25519_key"). Only used when SSHAddr is set.
	SSHHostKey string

	// RateLimit is the per-IP request rate limit in requests per second.
	// 0 disables rate limiting (default). Set via CODEFORT_RATE_LIMIT.
	RateLimit float64
}

// Load reads CODEFORT_* environment variables and resolves the data directory
// to an absolute path. It performs no filesystem side effects — call
// EnsureDirs separately before opening storage.
func Load() (*Config, error) {
	cfg := &Config{
		Addr:    envOr("CODEFORT_ADDR", ":8080"),
		DataDir: envOr("CODEFORT_DATA_DIR", "data"),
	}

	dataDir, err := filepath.Abs(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	cfg.DataDir = dataDir
	cfg.DBPath = envOr("CODEFORT_DB_PATH", filepath.Join(dataDir, "moongit.db"))
	cfg.ReposDir = envOr("CODEFORT_REPOS_DIR", filepath.Join(dataDir, "repos"))
	cfg.HostDataDir = envOr("CODEFORT_HOST_DATA_DIR", dataDir)
	cfg.WebDir = envOr("CODEFORT_WEB_DIR", "")
	cfg.BasicUser = envOr("CODEFORT_BASIC_USER", "")
	cfg.BasicPass = envOr("CODEFORT_BASIC_PASS", "")
	cfg.SSHAddr = envOr("CODEFORT_SSH_ADDR", "")
	cfg.SSHHostKey = envOr("CODEFORT_SSH_HOST_KEY", filepath.Join(dataDir, "ssh_host_ed25519_key"))

	rateLimit, err := strconv.ParseFloat(envOr("CODEFORT_RATE_LIMIT", "0"), 64)
	if err != nil {
		return nil, fmt.Errorf("CODEFORT_RATE_LIMIT: %w", err)
	}
	// 0 disables rate limiting; a negative is meaningless and previously
	// disabled it too, so a typo'd limit looked like a deliberate one. Load is
	// where a bad value should be loud.
	if rateLimit < 0 {
		return nil, fmt.Errorf("CODEFORT_RATE_LIMIT: %g is negative (use 0 to disable)", rateLimit)
	}
	cfg.RateLimit = rateLimit

	lease, err := time.ParseDuration(envOr("CODEFORT_CLAIM_LEASE", "60m"))
	if err != nil {
		return nil, fmt.Errorf("CODEFORT_CLAIM_LEASE: %w", err)
	}
	cfg.ClaimLease = lease

	ttl, err := time.ParseDuration(envOr("CODEFORT_AGENT_TOKEN_TTL", "168h"))
	if err != nil {
		return nil, fmt.Errorf("CODEFORT_AGENT_TOKEN_TTL: %w", err)
	}
	cfg.AgentTokenTTL = ttl

	ciTimeout, err := time.ParseDuration(envOr("CODEFORT_CI_RUN_TIMEOUT", "15m"))
	if err != nil {
		return nil, fmt.Errorf("CODEFORT_CI_RUN_TIMEOUT: %w", err)
	}
	cfg.CIRunTimeout = ciTimeout

	ciPoll, err := time.ParseDuration(envOr("CODEFORT_CI_POLL_INTERVAL", "5s"))
	if err != nil {
		return nil, fmt.Errorf("CODEFORT_CI_POLL_INTERVAL: %w", err)
	}
	cfg.CIPollInterval = ciPoll

	jobConc, err := strconv.Atoi(envOr("CODEFORT_CI_JOB_CONCURRENCY", "4"))
	if err != nil {
		return nil, fmt.Errorf("CODEFORT_CI_JOB_CONCURRENCY: %w", err)
	}
	cfg.CIJobConcurrency = jobConc

	maxConc, err := strconv.Atoi(envOr("CODEFORT_MAX_CONCURRENCY", strconv.Itoa(runtime.NumCPU())))
	if err != nil {
		return nil, fmt.Errorf("CODEFORT_MAX_CONCURRENCY: %w", err)
	}
	cfg.MaxConcurrency = maxConc

	retainRuns, err := strconv.Atoi(envOr("CODEFORT_CI_RETAIN_RUNS", "50"))
	if err != nil {
		return nil, fmt.Errorf("CODEFORT_CI_RETAIN_RUNS: %w", err)
	}
	cfg.CIRetainRuns = retainRuns

	eventRetain, err := strconv.Atoi(envOr("CODEFORT_EVENT_RETAIN", "10000"))
	if err != nil {
		return nil, fmt.Errorf("CODEFORT_EVENT_RETAIN: %w", err)
	}
	cfg.EventRetain = eventRetain

	cfg.CISecret = envOr("CODEFORT_CI_SECRET", "")

	cfg.CIIsolation = envOr("CODEFORT_CI_ISOLATION", "docker")
	switch cfg.CIIsolation {
	case "docker", "none":
	default:
		return nil, fmt.Errorf("CODEFORT_CI_ISOLATION: want \"docker\" or \"none\", got %q", cfg.CIIsolation)
	}
	cfg.CIDefaultImage = envOr("CODEFORT_CI_DEFAULT_IMAGE", "moongit-ci:latest")

	agentReserved, err := strconv.Atoi(envOr("CODEFORT_AGENT_RESERVED", "2"))
	if err != nil {
		return nil, fmt.Errorf("CODEFORT_AGENT_RESERVED: %w", err)
	}
	cfg.AgentReserved = agentReserved

	agentTimeout, err := time.ParseDuration(envOr("CODEFORT_AGENT_RUN_TIMEOUT", "60m"))
	if err != nil {
		return nil, fmt.Errorf("CODEFORT_AGENT_RUN_TIMEOUT: %w", err)
	}
	cfg.AgentRunTimeout = agentTimeout

	cfg.AgentDefaultImage = envOr("CODEFORT_AGENT_DEFAULT_IMAGE", "moongit-agent:latest")
	cfg.AgentClaudeOAuthToken = envOr("CODEFORT_AGENT_CLAUDE_OAUTH_TOKEN", "")
	cfg.AgentAnthropicAPIKey = envOr("CODEFORT_AGENT_ANTHROPIC_API_KEY", "")
	cfg.AgentLLMBaseURL = envOr("CODEFORT_AGENT_LLM_BASE_URL", "")
	cfg.AgentServerURL = envOr("CODEFORT_AGENT_SERVER_URL", "")

	agentTurnTimeout, err := time.ParseDuration(envOr("CODEFORT_AGENT_TURN_TIMEOUT", "15m"))
	if err != nil {
		return nil, fmt.Errorf("CODEFORT_AGENT_TURN_TIMEOUT: %w", err)
	}
	cfg.AgentTurnTimeout = agentTurnTimeout

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

// StaleEnv returns the names of any MOONGIT_* variables still set in the
// environment, sorted. The rename to CODEFORT_* was a hard cut with no
// fallback read, which means a leftover MOONGIT_DATA_DIR is not an error —
// Load simply takes the default and the server comes up against the wrong
// data directory, silently. This turns that silence into a startup warning.
//
// It reads the environment rather than taking it as an argument only because
// Load already does; the caller logs, so this stays free of a logger.
func StaleEnv() []string {
	var stale []string
	for _, kv := range os.Environ() {
		name, _, ok := strings.Cut(kv, "=")
		if ok && strings.HasPrefix(name, "MOONGIT_") {
			stale = append(stale, name)
		}
	}
	sort.Strings(stale)
	return stale
}
