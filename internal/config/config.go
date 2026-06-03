package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
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

	// CIJobConcurrency caps how many of a run's jobs execute at once: the
	// runner schedules jobs in dependency waves and runs every ready job (all
	// needs satisfied) concurrently up to this many. Set via
	// MOONGIT_CI_JOB_CONCURRENCY (default 4); values < 1 are treated as 1.
	CIJobConcurrency int

	// MaxConcurrency caps how many runs execute at once across one shared budget
	// that CI and agent runs both draw from. Set via MOONGIT_MAX_CONCURRENCY
	// (default runtime.NumCPU()); values < 1 are treated as 1. A CI run still
	// bounds its own jobs by CIJobConcurrency, so this caps concurrent *runs*,
	// not strictly job containers. Replaces the former independent CI/agent caps.
	MaxConcurrency int

	// CISecret gates the loopback /internal/ci/events endpoint that the
	// post-receive hook calls. Set via MOONGIT_CI_SECRET; empty means the
	// server generates a fresh per-process secret at startup (sufficient,
	// since it's injected into the hook env at push time and never persisted).
	CISecret string

	// CIIsolation selects how the runner executes a job's steps. "docker"
	// (default) runs each job in a throwaway container so repo-authored
	// commands never touch the host; "none" runs them on the host as the
	// moongitd user (the legacy path — RCE by design, use only when you trust
	// every CI-enabled repo). Set via MOONGIT_CI_ISOLATION.
	CIIsolation string

	// CIDefaultImage is the container image a job runs in when its mgitci.yml
	// doesn't set `image:`. It must be glibc-based and carry `mooncake` (and
	// `git`) on PATH — see ci/Dockerfile. Only used when CIIsolation="docker".
	// Set via MOONGIT_CI_DEFAULT_IMAGE.
	CIDefaultImage string

	// AgentReserved is how many of MaxConcurrency's slots only agent-family runs
	// may take, so a CI backlog can never lock out an agent spawn (the inverse
	// starves too — see the runner's fair, non-blocking drain). Set via
	// MOONGIT_AGENT_RESERVED (default 2); clamped to [0, MaxConcurrency].
	AgentReserved int

	// AgentRunTimeout is the whole-session lifetime cap for an agent run: a run
	// parked in awaiting_input is reaped (container torn down, run finalized)
	// once it's older than this, so an abandoned session can't hold a container
	// forever. Set via MOONGIT_AGENT_RUN_TIMEOUT (default 60m). Zero or negative
	// disables the reaper.
	AgentRunTimeout time.Duration

	// AgentTurnTimeout is the per-turn wall-clock limit: one claude invocation
	// (turn 1 or a follow-up) runs under this deadline; an overrunning turn is
	// killed and the turn errored. Set via MOONGIT_AGENT_TURN_TIMEOUT (default
	// 15m). Zero or negative disables the per-turn deadline.
	AgentTurnTimeout time.Duration

	// AgentDefaultImage is the container image an agent run executes in: the CI
	// base image plus the Claude CLI and the dex MCP shim (#75). Set via
	// MOONGIT_AGENT_DEFAULT_IMAGE. Only used when CIIsolation="docker".
	AgentDefaultImage string

	// Agent credentials, injected per-run into the container env — never baked
	// into the image (#77). AgentClaudeOAuthToken is the subscription token from
	// `claude setup-token` (CLAUDE_CODE_OAUTH_TOKEN); AgentAnthropicAPIKey is the
	// alternate API-key path (ANTHROPIC_API_KEY); exactly one is needed for the
	// agent to authenticate headlessly. AgentLLMBaseURL optionally overrides the
	// LLM endpoint (ANTHROPIC_BASE_URL — Anthropic now, a local GPU model later).
	// Set via MOONGIT_AGENT_CLAUDE_OAUTH_TOKEN / _ANTHROPIC_API_KEY / _LLM_BASE_URL.
	AgentClaudeOAuthToken string
	AgentAnthropicAPIKey  string
	AgentLLMBaseURL       string

	// AgentServerURL is how the in-container agent reaches this moongitd (for
	// mgit / git over the host gateway). Empty defaults to
	// http://host.docker.internal:<port-of-Addr>. Set via MOONGIT_AGENT_SERVER_URL.
	AgentServerURL string

	// AgentMooncakeMaxIterations is the step-loop backstop for a mooncake-agent
	// turn (`mooncake agent run --max-iterations`). Under --style step the loop
	// is meant to end on its own terminal signal (goal reached / stall); this is
	// just the ceiling for a planner that never converges, with the per-turn
	// wall-clock (AgentTurnTimeout) as the real governor. Defaults high (run
	// until done); set a lower value via MOONGIT_AGENT_MOONCAKE_MAX_ITERATIONS to
	// pin a tighter cap. Only used by the mooncake-agent execution model (#110).
	AgentMooncakeMaxIterations int

	// mooncake-agent policy (#110/#11): mooncake enforces these per run at
	// executor preflight, re-establishing the execution wall that moving off
	// Claude's managed Bash policy loses. A denied step fails the run before
	// any side effect. DenyActions defaults to {shell,cmd} (the agent uses
	// typed actions, not a raw shell) and is cleared by setting an empty
	// MOONGIT_AGENT_MOONCAKE_DENY_ACTIONS. AllowActions is an optional allowlist
	// (deny wins). DenyNetwork refuses egress steps; MaxRisk (1..10, 0=off)
	// caps a step's estimated risk band. Set via MOONGIT_AGENT_MOONCAKE_{ALLOW,
	// DENY}_ACTIONS (comma-sep), _DENY_NETWORK, _MAX_RISK.
	AgentMooncakeAllowActions []string
	AgentMooncakeDenyActions  []string
	AgentMooncakeDenyNetwork  bool
	AgentMooncakeMaxRisk      int

	// DexProject is the dex project id (keyed by the canonical repo root) the
	// agent's dex MCP queries. Empty omits the dex MCP wiring. Set via
	// MOONGIT_AGENT_DEX_PROJECT.
	DexProject string

	// CIRetainRuns caps how many of a repo's most recent CI runs are kept: a
	// periodic reaper prunes terminal runs beyond this many (and their on-disk
	// event logs), keeping disk + DB bounded. queued/running runs are never
	// pruned. Set via MOONGIT_CI_RETAIN_RUNS (default 50); zero or negative
	// disables retention.
	CIRetainRuns int

	// EventRetain caps how many of the most recent outbound feed events (#73)
	// are kept: a periodic reaper prunes older rows, bounding the events table.
	// Set via MOONGIT_EVENT_RETAIN (default 10000); zero or negative disables
	// retention.
	EventRetain int

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

	// SSHAddr is the listen address for the opt-in git SSH transport (e.g.
	// ":2222"). Empty (default) disables SSH entirely, keeping moongitd a
	// single HTTP port. When set, moongitd serves git over SSH with publickey
	// auth against registered keys. Set via MOONGIT_SSH_ADDR.
	SSHAddr string

	// SSHHostKey is the path to the persisted SSH host private key. It's
	// generated (ed25519, 0600) on first use if absent so the host identity is
	// stable across restarts. Set via MOONGIT_SSH_HOST_KEY (default
	// "$MOONGIT_DATA_DIR/ssh_host_ed25519_key"). Only used when SSHAddr is set.
	SSHHostKey string
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
	cfg.SSHAddr = envOr("MOONGIT_SSH_ADDR", "")
	cfg.SSHHostKey = envOr("MOONGIT_SSH_HOST_KEY", filepath.Join(dataDir, "ssh_host_ed25519_key"))

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

	jobConc, err := strconv.Atoi(envOr("MOONGIT_CI_JOB_CONCURRENCY", "4"))
	if err != nil {
		return nil, fmt.Errorf("MOONGIT_CI_JOB_CONCURRENCY: %w", err)
	}
	cfg.CIJobConcurrency = jobConc

	maxConc, err := strconv.Atoi(envOr("MOONGIT_MAX_CONCURRENCY", strconv.Itoa(runtime.NumCPU())))
	if err != nil {
		return nil, fmt.Errorf("MOONGIT_MAX_CONCURRENCY: %w", err)
	}
	cfg.MaxConcurrency = maxConc

	retainRuns, err := strconv.Atoi(envOr("MOONGIT_CI_RETAIN_RUNS", "50"))
	if err != nil {
		return nil, fmt.Errorf("MOONGIT_CI_RETAIN_RUNS: %w", err)
	}
	cfg.CIRetainRuns = retainRuns

	eventRetain, err := strconv.Atoi(envOr("MOONGIT_EVENT_RETAIN", "10000"))
	if err != nil {
		return nil, fmt.Errorf("MOONGIT_EVENT_RETAIN: %w", err)
	}
	cfg.EventRetain = eventRetain

	cfg.CISecret = envOr("MOONGIT_CI_SECRET", "")

	cfg.CIIsolation = envOr("MOONGIT_CI_ISOLATION", "docker")
	switch cfg.CIIsolation {
	case "docker", "none":
	default:
		return nil, fmt.Errorf("MOONGIT_CI_ISOLATION: want \"docker\" or \"none\", got %q", cfg.CIIsolation)
	}
	cfg.CIDefaultImage = envOr("MOONGIT_CI_DEFAULT_IMAGE", "moongit-ci:latest")

	agentReserved, err := strconv.Atoi(envOr("MOONGIT_AGENT_RESERVED", "2"))
	if err != nil {
		return nil, fmt.Errorf("MOONGIT_AGENT_RESERVED: %w", err)
	}
	cfg.AgentReserved = agentReserved

	agentTimeout, err := time.ParseDuration(envOr("MOONGIT_AGENT_RUN_TIMEOUT", "60m"))
	if err != nil {
		return nil, fmt.Errorf("MOONGIT_AGENT_RUN_TIMEOUT: %w", err)
	}
	cfg.AgentRunTimeout = agentTimeout

	cfg.AgentDefaultImage = envOr("MOONGIT_AGENT_DEFAULT_IMAGE", "moongit-agent:latest")
	cfg.AgentClaudeOAuthToken = envOr("MOONGIT_AGENT_CLAUDE_OAUTH_TOKEN", "")
	cfg.AgentAnthropicAPIKey = envOr("MOONGIT_AGENT_ANTHROPIC_API_KEY", "")
	cfg.AgentLLMBaseURL = envOr("MOONGIT_AGENT_LLM_BASE_URL", "")
	cfg.AgentServerURL = envOr("MOONGIT_AGENT_SERVER_URL", "")
	cfg.DexProject = envOr("MOONGIT_AGENT_DEX_PROJECT", "")

	agentTurnTimeout, err := time.ParseDuration(envOr("MOONGIT_AGENT_TURN_TIMEOUT", "15m"))
	if err != nil {
		return nil, fmt.Errorf("MOONGIT_AGENT_TURN_TIMEOUT: %w", err)
	}
	cfg.AgentTurnTimeout = agentTurnTimeout

	mooncakeIters, err := strconv.Atoi(envOr("MOONGIT_AGENT_MOONCAKE_MAX_ITERATIONS", "3"))
	if err != nil {
		return nil, fmt.Errorf("MOONGIT_AGENT_MOONCAKE_MAX_ITERATIONS: %w", err)
	}
	cfg.AgentMooncakeMaxIterations = mooncakeIters

	// Mooncake policy. DenyActions defaults to {shell,cmd}; use LookupEnv (not
	// envOr) so an explicit empty value clears the default to opt into shell.
	denyRaw := "shell,cmd"
	if v, ok := os.LookupEnv("MOONGIT_AGENT_MOONCAKE_DENY_ACTIONS"); ok {
		denyRaw = v
	}
	cfg.AgentMooncakeDenyActions = splitCSV(denyRaw)
	cfg.AgentMooncakeAllowActions = splitCSV(os.Getenv("MOONGIT_AGENT_MOONCAKE_ALLOW_ACTIONS"))
	cfg.AgentMooncakeDenyNetwork = envOr("MOONGIT_AGENT_MOONCAKE_DENY_NETWORK", "false") == "true"
	mooncakeRisk, err := strconv.Atoi(envOr("MOONGIT_AGENT_MOONCAKE_MAX_RISK", "0"))
	if err != nil {
		return nil, fmt.Errorf("MOONGIT_AGENT_MOONCAKE_MAX_RISK: %w", err)
	}
	cfg.AgentMooncakeMaxRisk = mooncakeRisk

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

// splitCSV parses a comma-separated env value into a trimmed, empty-dropped
// slice. Returns nil for an empty/blank input so callers can treat "unset" and
// "no entries" the same.
func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
