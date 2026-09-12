package config

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// allVars is every CODEFORT_* variable Load reads. Tests neutralise the whole
// set before asserting, so an operator's ambient environment can never make a
// default-case test pass or fail spuriously. Setting a var to "" is the
// documented way to say "unset" (envOr treats empty as absent), which is why
// clearEnv can use t.Setenv rather than unsetting.
var allVars = []string{
	"CODEFORT_ADDR",
	"CODEFORT_DATA_DIR",
	"CODEFORT_DB_PATH",
	"CODEFORT_REPOS_DIR",
	"CODEFORT_HOST_DATA_DIR",
	"CODEFORT_WEB_DIR",
	"CODEFORT_BASIC_USER",
	"CODEFORT_BASIC_PASS",
	"CODEFORT_SSH_ADDR",
	"CODEFORT_SSH_HOST_KEY",
	"CODEFORT_RATE_LIMIT",
	"CODEFORT_CLAIM_LEASE",
	"CODEFORT_AGENT_TOKEN_TTL",
	"CODEFORT_CI_RUN_TIMEOUT",
	"CODEFORT_CI_POLL_INTERVAL",
	"CODEFORT_CI_JOB_CONCURRENCY",
	"CODEFORT_MAX_CONCURRENCY",
	"CODEFORT_CI_RETAIN_RUNS",
	"CODEFORT_EVENT_RETAIN",
	"CODEFORT_CI_SECRET",
	"CODEFORT_CI_ISOLATION",
	"CODEFORT_CI_DEFAULT_IMAGE",
	"CODEFORT_AGENT_RESERVED",
	"CODEFORT_AGENT_RUN_TIMEOUT",
	"CODEFORT_AGENT_DEFAULT_IMAGE",
	"CODEFORT_AGENT_CLAUDE_OAUTH_TOKEN",
	"CODEFORT_AGENT_ANTHROPIC_API_KEY",
	"CODEFORT_AGENT_LLM_BASE_URL",
	"CODEFORT_AGENT_SERVER_URL",
	"CODEFORT_AGENT_TURN_TIMEOUT",
}

// clearEnv blanks every knob. t.Setenv also forbids t.Parallel, which is
// correct here: Load reads process-global state.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range allVars {
		t.Setenv(k, "")
	}
}

// load runs Load with a cleared environment plus the given overrides, and
// fails the test if Load errors.
func load(t *testing.T, kv ...string) *Config {
	t.Helper()
	clearEnv(t)
	for i := 0; i < len(kv); i += 2 {
		t.Setenv(kv[i], kv[i+1])
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

func TestLoadDefaults(t *testing.T) {
	// Pin the whole default surface in one place: a deployment that sets
	// nothing gets exactly this, and any future default change has to be
	// deliberate enough to edit this table.
	dataDir := t.TempDir()
	cfg := load(t, "CODEFORT_DATA_DIR", dataDir)

	strs := []struct {
		field string
		got   string
		want  string
	}{
		{"Addr", cfg.Addr, ":8080"},
		{"DataDir", cfg.DataDir, dataDir},
		{"DBPath", cfg.DBPath, filepath.Join(dataDir, "moongit.db")},
		{"ReposDir", cfg.ReposDir, filepath.Join(dataDir, "repos")},
		{"HostDataDir", cfg.HostDataDir, dataDir},
		{"SSHHostKey", cfg.SSHHostKey, filepath.Join(dataDir, "ssh_host_ed25519_key")},
		{"CIIsolation", cfg.CIIsolation, "docker"},
		{"CIDefaultImage", cfg.CIDefaultImage, "codefort-ci:latest"},
		{"AgentDefaultImage", cfg.AgentDefaultImage, "codefort-agent:latest"},
		// Everything below defaults to empty: each empty default is a feature
		// that stays off unless explicitly turned on (web UI, basic auth, SSH,
		// agent credentials).
		{"WebDir", cfg.WebDir, ""},
		{"BasicUser", cfg.BasicUser, ""},
		{"BasicPass", cfg.BasicPass, ""},
		{"SSHAddr", cfg.SSHAddr, ""},
		{"CISecret", cfg.CISecret, ""},
		{"AgentClaudeOAuthToken", cfg.AgentClaudeOAuthToken, ""},
		{"AgentAnthropicAPIKey", cfg.AgentAnthropicAPIKey, ""},
		{"AgentLLMBaseURL", cfg.AgentLLMBaseURL, ""},
		{"AgentServerURL", cfg.AgentServerURL, ""},
	}
	for _, c := range strs {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.field, c.got, c.want)
		}
	}

	durs := []struct {
		field string
		got   time.Duration
		want  time.Duration
	}{
		{"ClaimLease", cfg.ClaimLease, 60 * time.Minute},
		{"AgentTokenTTL", cfg.AgentTokenTTL, 168 * time.Hour},
		{"CIRunTimeout", cfg.CIRunTimeout, 15 * time.Minute},
		{"CIPollInterval", cfg.CIPollInterval, 5 * time.Second},
		{"AgentRunTimeout", cfg.AgentRunTimeout, 60 * time.Minute},
		{"AgentTurnTimeout", cfg.AgentTurnTimeout, 15 * time.Minute},
	}
	for _, c := range durs {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.field, c.got, c.want)
		}
	}

	ints := []struct {
		field string
		got   int
		want  int
	}{
		{"CIJobConcurrency", cfg.CIJobConcurrency, 4},
		// The default run budget tracks the box, not a constant.
		{"MaxConcurrency", cfg.MaxConcurrency, runtime.NumCPU()},
		{"CIRetainRuns", cfg.CIRetainRuns, 50},
		{"EventRetain", cfg.EventRetain, 10000},
		{"AgentReserved", cfg.AgentReserved, 2},
	}
	for _, c := range ints {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.field, c.got, c.want)
		}
	}

	if cfg.RateLimit != 0 {
		t.Errorf("RateLimit = %v, want 0 (rate limiting off by default)", cfg.RateLimit)
	}
}

func TestLoadDataDirDefaultsRelativeToCWD(t *testing.T) {
	// With no CODEFORT_DATA_DIR the server stores everything under "data"
	// relative to the process CWD — fragile under systemd, hence documented.
	// Load resolves it to an absolute path so nothing downstream re-resolves
	// against a different CWD later.
	dir := t.TempDir()
	t.Chdir(dir)
	cfg := load(t)

	want, err := filepath.Abs("data")
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	if cfg.DataDir != want {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, want)
	}
	if !filepath.IsAbs(cfg.DataDir) {
		t.Errorf("DataDir %q is not absolute", cfg.DataDir)
	}
}

func TestLoadDataDirRelativeIsAbsolutised(t *testing.T) {
	// A relative CODEFORT_DATA_DIR must be absolutised *before* DBPath,
	// ReposDir, HostDataDir and SSHHostKey are derived from it — otherwise a
	// later chdir would silently split the daemon's state across two trees.
	dir := t.TempDir()
	t.Chdir(dir)
	cfg := load(t, "CODEFORT_DATA_DIR", "srv/state")

	want := filepath.Join(dir, "srv", "state")
	// macOS /tmp is a symlink to /private/tmp; compare resolved forms.
	if resolve(t, cfg.DataDir) != resolve(t, want) {
		t.Fatalf("DataDir = %q, want %q", cfg.DataDir, want)
	}
	for _, c := range []struct{ field, got string }{
		{"DBPath", cfg.DBPath},
		{"ReposDir", cfg.ReposDir},
		{"SSHHostKey", cfg.SSHHostKey},
		{"HostDataDir", cfg.HostDataDir},
	} {
		if !strings.HasPrefix(c.got, cfg.DataDir) {
			t.Errorf("%s = %q, want a path under the absolutised DataDir %q", c.field, c.got, cfg.DataDir)
		}
	}
}

func TestLoadPathOverridesBeatDerivation(t *testing.T) {
	// DB and repos may live on different storage than the rest of the data
	// dir; explicit settings must win over the derived defaults, and must not
	// drag HostDataDir/SSHHostKey with them.
	dataDir := t.TempDir()
	db := filepath.Join(t.TempDir(), "elsewhere.db")
	repos := filepath.Join(t.TempDir(), "bare")
	cfg := load(t,
		"CODEFORT_DATA_DIR", dataDir,
		"CODEFORT_DB_PATH", db,
		"CODEFORT_REPOS_DIR", repos,
	)

	if cfg.DBPath != db {
		t.Errorf("DBPath = %q, want %q", cfg.DBPath, db)
	}
	if cfg.ReposDir != repos {
		t.Errorf("ReposDir = %q, want %q", cfg.ReposDir, repos)
	}
	// Overriding DB/repos leaves the remaining derivations anchored on DataDir.
	if want := filepath.Join(dataDir, "ssh_host_ed25519_key"); cfg.SSHHostKey != want {
		t.Errorf("SSHHostKey = %q, want %q", cfg.SSHHostKey, want)
	}
	if cfg.HostDataDir != dataDir {
		t.Errorf("HostDataDir = %q, want %q", cfg.HostDataDir, dataDir)
	}
	// Explicit paths are taken verbatim — no Abs() pass, unlike DataDir.
	// A relative CODEFORT_DB_PATH therefore stays relative to the CWD.
	dir := t.TempDir()
	t.Chdir(dir)
	rel := load(t, "CODEFORT_DATA_DIR", dataDir, "CODEFORT_DB_PATH", "moongit.db")
	if rel.DBPath != "moongit.db" {
		t.Errorf("relative DBPath = %q, want it kept verbatim as %q", rel.DBPath, "moongit.db")
	}
}

func TestLoadHostDataDirOverride(t *testing.T) {
	// Containerised codefortd: DataDir is the in-container path, HostDataDir is
	// where the host Docker daemon must resolve bind-mount sources. Getting
	// this wrong makes sibling containers mount paths that don't exist.
	dataDir := t.TempDir()
	cfg := load(t,
		"CODEFORT_DATA_DIR", dataDir,
		"CODEFORT_HOST_DATA_DIR", "/home/ops/.local/share/codefort",
	)
	if cfg.HostDataDir != "/home/ops/.local/share/codefort" {
		t.Errorf("HostDataDir = %q, want the host path", cfg.HostDataDir)
	}
	if cfg.DataDir != dataDir {
		t.Errorf("HostDataDir override leaked into DataDir = %q", cfg.DataDir)
	}
}

func TestLoadEmptyStringIsUnset(t *testing.T) {
	// `VAR=` cannot blank out a defaulted value: envOr checks for "" as well
	// as absence. Operationally this means an empty export in a systemd unit
	// is a no-op, not a way to disable a feature — use the documented
	// disabling value instead.
	dataDir := t.TempDir()
	cfg := load(t,
		"CODEFORT_DATA_DIR", dataDir,
		"CODEFORT_ADDR", "",
		"CODEFORT_CI_ISOLATION", "",
		"CODEFORT_CLAIM_LEASE", "",
		"CODEFORT_MAX_CONCURRENCY", "",
		"CODEFORT_CI_DEFAULT_IMAGE", "",
	)
	if cfg.Addr != ":8080" {
		t.Errorf("Addr = %q, want the default %q", cfg.Addr, ":8080")
	}
	if cfg.CIIsolation != "docker" {
		t.Errorf("CIIsolation = %q, want the default %q", cfg.CIIsolation, "docker")
	}
	if cfg.ClaimLease != 60*time.Minute {
		t.Errorf("ClaimLease = %v, want the default 60m", cfg.ClaimLease)
	}
	if cfg.MaxConcurrency != runtime.NumCPU() {
		t.Errorf("MaxConcurrency = %d, want NumCPU %d", cfg.MaxConcurrency, runtime.NumCPU())
	}
	if cfg.CIDefaultImage != "codefort-ci:latest" {
		t.Errorf("CIDefaultImage = %q, want the default", cfg.CIDefaultImage)
	}
	// Same rule applies to the path derivation: DB_PATH="" still derives.
	if want := filepath.Join(dataDir, "moongit.db"); cfg.DBPath != want {
		t.Errorf("DBPath = %q, want %q", cfg.DBPath, want)
	}
}

func TestLoadDurationsParse(t *testing.T) {
	// Go duration syntax throughout; each knob is read independently, so a
	// per-variable table is enough to catch a copy-paste wiring mistake (e.g.
	// two fields reading the same env var).
	cases := []struct {
		env  string
		val  string
		get  func(*Config) time.Duration
		want time.Duration
	}{
		{"CODEFORT_CLAIM_LEASE", "90s", func(c *Config) time.Duration { return c.ClaimLease }, 90 * time.Second},
		{"CODEFORT_AGENT_TOKEN_TTL", "24h", func(c *Config) time.Duration { return c.AgentTokenTTL }, 24 * time.Hour},
		{"CODEFORT_CI_RUN_TIMEOUT", "1h30m", func(c *Config) time.Duration { return c.CIRunTimeout }, 90 * time.Minute},
		{"CODEFORT_CI_POLL_INTERVAL", "250ms", func(c *Config) time.Duration { return c.CIPollInterval }, 250 * time.Millisecond},
		{"CODEFORT_AGENT_RUN_TIMEOUT", "2h", func(c *Config) time.Duration { return c.AgentRunTimeout }, 2 * time.Hour},
		{"CODEFORT_AGENT_TURN_TIMEOUT", "45m", func(c *Config) time.Duration { return c.AgentTurnTimeout }, 45 * time.Minute},
	}
	for _, c := range cases {
		t.Run(c.env, func(t *testing.T) {
			cfg := load(t, c.env, c.val)
			if got := c.get(cfg); got != c.want {
				t.Errorf("%s=%q gave %v, want %v", c.env, c.val, got, c.want)
			}
		})
	}
}

func TestLoadRejectsBadValues(t *testing.T) {
	// Parse failures abort startup rather than falling back to the default:
	// a typo'd timeout must not boot a daemon that merely *looks* healthy.
	// The error names the offending variable so the operator can find it.
	cases := []struct{ env, val string }{
		// "15" without a unit is the classic mistake — Go needs "15m".
		{"CODEFORT_CI_RUN_TIMEOUT", "15"},
		{"CODEFORT_CLAIM_LEASE", "an hour"},
		{"CODEFORT_AGENT_TOKEN_TTL", "7d"}, // Go has no "d" unit
		{"CODEFORT_CI_POLL_INTERVAL", "5 s"},
		{"CODEFORT_AGENT_RUN_TIMEOUT", "-"},
		{"CODEFORT_AGENT_TURN_TIMEOUT", "15min"},
		{"CODEFORT_RATE_LIMIT", "fast"},
		{"CODEFORT_CI_JOB_CONCURRENCY", "4.5"}, // Atoi, not ParseFloat
		{"CODEFORT_MAX_CONCURRENCY", "many"},
		{"CODEFORT_CI_RETAIN_RUNS", "50 runs"},
		{"CODEFORT_EVENT_RETAIN", "10_000"},
	}
	for _, c := range cases {
		t.Run(c.env+"="+c.val, func(t *testing.T) {
			clearEnv(t)
			t.Setenv(c.env, c.val)
			cfg, err := Load()
			if err == nil {
				t.Fatalf("%s=%q loaded without error (cfg=%+v), want a startup failure", c.env, c.val, cfg)
			}
			if !strings.Contains(err.Error(), c.env) {
				t.Errorf("error %q does not name %s", err, c.env)
			}
		})
	}
	// The empty-string case is the counterexample: it is unset, not invalid.
	cfg := load(t, "CODEFORT_AGENT_RESERVED", "")
	if cfg.AgentReserved != 2 {
		t.Errorf("AgentReserved = %d for empty value, want the default 2", cfg.AgentReserved)
	}
}

func TestLoadCIIsolationEnum(t *testing.T) {
	// "none" runs repo-authored commands on the host — RCE by design — so the
	// only safe failure mode for a typo is refusing to boot, never silently
	// picking one of the two.
	for _, v := range []string{"docker", "none"} {
		cfg := load(t, "CODEFORT_CI_ISOLATION", v)
		if cfg.CIIsolation != v {
			t.Errorf("CIIsolation = %q, want %q", cfg.CIIsolation, v)
		}
	}
	for _, v := range []string{"Docker", "DOCKER", "podman", "off", "false", " none", "none "} {
		clearEnv(t)
		t.Setenv("CODEFORT_CI_ISOLATION", v)
		if _, err := Load(); err == nil {
			t.Errorf("CODEFORT_CI_ISOLATION=%q was accepted; want rejection (matching is exact, no trim/fold)", v)
		}
	}
}

func TestLoadDisableSentinels(t *testing.T) {
	// The "0 means off" knobs: they must survive Load as zero/negative, since
	// the consumers read exactly that to skip a reaper or a deadline. A Load
	// that helpfully clamped these to 1 would turn "disabled" into "every
	// nanosecond".
	cfg := load(t,
		"CODEFORT_CLAIM_LEASE", "0",
		"CODEFORT_AGENT_TOKEN_TTL", "0",
		"CODEFORT_CI_RUN_TIMEOUT", "0",
		"CODEFORT_AGENT_RUN_TIMEOUT", "0",
		"CODEFORT_AGENT_TURN_TIMEOUT", "0",
		"CODEFORT_CI_RETAIN_RUNS", "0",
		"CODEFORT_EVENT_RETAIN", "0",
		"CODEFORT_RATE_LIMIT", "0",
	)
	if cfg.ClaimLease != 0 {
		t.Errorf("ClaimLease = %v, want 0 (claims never expire)", cfg.ClaimLease)
	}
	if cfg.AgentTokenTTL != 0 {
		t.Errorf("AgentTokenTTL = %v, want 0 (token reaper off)", cfg.AgentTokenTTL)
	}
	if cfg.CIRunTimeout != 0 {
		t.Errorf("CIRunTimeout = %v, want 0 (no wall-clock limit)", cfg.CIRunTimeout)
	}
	if cfg.AgentRunTimeout != 0 {
		t.Errorf("AgentRunTimeout = %v, want 0 (session reaper off)", cfg.AgentRunTimeout)
	}
	if cfg.AgentTurnTimeout != 0 {
		t.Errorf("AgentTurnTimeout = %v, want 0 (no per-turn deadline)", cfg.AgentTurnTimeout)
	}
	if cfg.CIRetainRuns != 0 {
		t.Errorf("CIRetainRuns = %d, want 0 (retention off)", cfg.CIRetainRuns)
	}
	if cfg.EventRetain != 0 {
		t.Errorf("EventRetain = %d, want 0 (retention off)", cfg.EventRetain)
	}
	if cfg.RateLimit != 0 {
		t.Errorf("RateLimit = %v, want 0 (no limiting)", cfg.RateLimit)
	}

	// Negative values are also "disabled" for the zero-or-negative knobs, and
	// Load passes them through untouched rather than normalising to 0.
	neg := load(t,
		"CODEFORT_CI_RUN_TIMEOUT", "-1s",
		"CODEFORT_AGENT_RUN_TIMEOUT", "-1m",
		"CODEFORT_AGENT_TURN_TIMEOUT", "-1h",
		"CODEFORT_CI_RETAIN_RUNS", "-1",
		"CODEFORT_EVENT_RETAIN", "-5",
	)
	if neg.CIRunTimeout != -time.Second {
		t.Errorf("CIRunTimeout = %v, want -1s passed through", neg.CIRunTimeout)
	}
	if neg.AgentRunTimeout != -time.Minute {
		t.Errorf("AgentRunTimeout = %v, want -1m passed through", neg.AgentRunTimeout)
	}
	if neg.AgentTurnTimeout != -time.Hour {
		t.Errorf("AgentTurnTimeout = %v, want -1h passed through", neg.AgentTurnTimeout)
	}
	if neg.CIRetainRuns != -1 {
		t.Errorf("CIRetainRuns = %d, want -1 passed through", neg.CIRetainRuns)
	}
	if neg.EventRetain != -5 {
		t.Errorf("EventRetain = %d, want -5 passed through", neg.EventRetain)
	}
}

func TestLoadConcurrencyIsNotClampedAtLoad(t *testing.T) {
	// Load is deliberately a dumb reader: the documented clamps
	// (MaxConcurrency/CIJobConcurrency floor of 1, AgentReserved into
	// [0, MaxConcurrency]) happen at the point of use — cmd/codefortd's
	// run loop and newWorkBudget — not here. Pinning that keeps anyone
	// from "fixing" Load and double-clamping, and documents that a Config
	// value read straight out of Load may still be out of range.
	cases := []struct {
		name             string
		env              []string
		jobConc, maxConc int
		reserved         int
	}{
		{
			name:     "below the floor",
			env:      []string{"CODEFORT_CI_JOB_CONCURRENCY", "0", "CODEFORT_MAX_CONCURRENCY", "-3", "CODEFORT_AGENT_RESERVED", "-4"},
			jobConc:  0,
			maxConc:  -3,
			reserved: -4,
		},
		{
			// AgentReserved above MaxConcurrency would starve CI entirely;
			// Load still accepts it and leaves the budget to clamp.
			name:     "reserved exceeds the shared budget",
			env:      []string{"CODEFORT_MAX_CONCURRENCY", "2", "CODEFORT_AGENT_RESERVED", "99"},
			jobConc:  4,
			maxConc:  2,
			reserved: 99,
		},
		{
			name:     "in range",
			env:      []string{"CODEFORT_CI_JOB_CONCURRENCY", "8", "CODEFORT_MAX_CONCURRENCY", "6", "CODEFORT_AGENT_RESERVED", "0"},
			jobConc:  8,
			maxConc:  6,
			reserved: 0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := load(t, c.env...)
			if cfg.CIJobConcurrency != c.jobConc {
				t.Errorf("CIJobConcurrency = %d, want %d", cfg.CIJobConcurrency, c.jobConc)
			}
			if cfg.MaxConcurrency != c.maxConc {
				t.Errorf("MaxConcurrency = %d, want %d", cfg.MaxConcurrency, c.maxConc)
			}
			if cfg.AgentReserved != c.reserved {
				t.Errorf("AgentReserved = %d, want %d", cfg.AgentReserved, c.reserved)
			}
		})
	}
}

func TestLoadRateLimitAcceptsFractionsRejectsNegatives(t *testing.T) {
	// Fractional rates are legitimate (a request every few seconds), so parsing
	// stays permissive there. A negative is not a slower limit — it is a typo
	// that used to disable rate limiting outright, which is the opposite of
	// what whoever typed it wanted. Load refuses it.
	cfg := load(t, "CODEFORT_RATE_LIMIT", "2.5")
	if cfg.RateLimit != 2.5 {
		t.Errorf("RateLimit = %v, want 2.5", cfg.RateLimit)
	}
	clearEnv(t)
	t.Setenv("CODEFORT_RATE_LIMIT", "-1")
	if _, err := Load(); err == nil {
		t.Error("Load accepted a negative CODEFORT_RATE_LIMIT, want error")
	}
}

func TestLoadPassthroughStrings(t *testing.T) {
	// Opaque strings (credentials, images, addresses) must arrive byte-exact:
	// a trimmed or lowercased token authenticates against nothing.
	cfg := load(t,
		"CODEFORT_ADDR", "127.0.0.1:9999",
		"CODEFORT_WEB_DIR", "/opt/codefort/web/dist",
		"CODEFORT_BASIC_USER", "Ops",
		"CODEFORT_BASIC_PASS", " p@ss word ",
		"CODEFORT_SSH_ADDR", ":2222",
		"CODEFORT_SSH_HOST_KEY", "/etc/codefort/hostkey",
		"CODEFORT_CI_SECRET", "s3cr3t",
		"CODEFORT_CI_DEFAULT_IMAGE", "ghcr.io/x/ci:v2",
		"CODEFORT_AGENT_DEFAULT_IMAGE", "ghcr.io/x/agent:v2",
		"CODEFORT_AGENT_CLAUDE_OAUTH_TOKEN", "sk-oauth",
		"CODEFORT_AGENT_ANTHROPIC_API_KEY", "sk-ant",
		"CODEFORT_AGENT_LLM_BASE_URL", "http://gw.local/v1",
		"CODEFORT_AGENT_SERVER_URL", "http://codefort.local:8080",
	)
	for _, c := range []struct{ field, got, want string }{
		{"Addr", cfg.Addr, "127.0.0.1:9999"},
		{"WebDir", cfg.WebDir, "/opt/codefort/web/dist"},
		{"BasicUser", cfg.BasicUser, "Ops"},
		{"BasicPass", cfg.BasicPass, " p@ss word "},
		{"SSHAddr", cfg.SSHAddr, ":2222"},
		{"SSHHostKey", cfg.SSHHostKey, "/etc/codefort/hostkey"},
		{"CISecret", cfg.CISecret, "s3cr3t"},
		{"CIDefaultImage", cfg.CIDefaultImage, "ghcr.io/x/ci:v2"},
		{"AgentDefaultImage", cfg.AgentDefaultImage, "ghcr.io/x/agent:v2"},
		{"AgentClaudeOAuthToken", cfg.AgentClaudeOAuthToken, "sk-oauth"},
		{"AgentAnthropicAPIKey", cfg.AgentAnthropicAPIKey, "sk-ant"},
		{"AgentLLMBaseURL", cfg.AgentLLMBaseURL, "http://gw.local/v1"},
		{"AgentServerURL", cfg.AgentServerURL, "http://codefort.local:8080"},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.field, c.got, c.want)
		}
	}
}

func TestLoadNoFilesystemSideEffects(t *testing.T) {
	// Load's contract: read-only. Callers (and tests) rely on being able to
	// load a config for a path that must not be created until EnsureDirs.
	base := t.TempDir()
	dataDir := filepath.Join(base, "state")
	cfg := load(t, "CODEFORT_DATA_DIR", dataDir)
	if _, err := statPath(dataDir); err == nil {
		t.Fatalf("Load created %s; it must have no filesystem side effects", dataDir)
	}

	if err := cfg.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs: %v", err)
	}
	for _, p := range []string{cfg.DataDir, cfg.ReposDir} {
		fi, err := statPath(p)
		if err != nil {
			t.Fatalf("EnsureDirs did not create %s: %v", p, err)
		}
		if !fi.IsDir() {
			t.Errorf("%s is not a directory", p)
		}
	}
	// Idempotent — startup re-runs it on every boot.
	if err := cfg.EnsureDirs(); err != nil {
		t.Errorf("second EnsureDirs: %v", err)
	}
}

// resolve returns p with symlinks resolved, so TempDir paths (/var -> /private
// /var on macOS) compare equal to paths built from the same CWD.
func resolve(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		return p
	}
	return r
}

func statPath(p string) (fs.FileInfo, error) { return os.Stat(p) }

func TestStaleEnvReportsPreRenameVars(t *testing.T) {
	t.Setenv("MOONGIT_DATA_DIR", "/old")
	t.Setenv("MOONGIT_ADDR", ":9999")
	t.Setenv("CODEFORT_ADDR", ":8080")

	got := StaleEnv()
	want := []string{"MOONGIT_ADDR", "MOONGIT_DATA_DIR"}
	if !slices.Equal(got, want) {
		t.Errorf("StaleEnv() = %v, want %v", got, want)
	}
}

// The whole point of the warning is that a leftover var changes nothing about
// the resulting config — it is ignored, which is exactly why it needs saying.
func TestStaleEnvDoesNotAffectLoad(t *testing.T) {
	t.Setenv("MOONGIT_ADDR", ":9999")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Addr != ":8080" {
		t.Errorf("Addr = %q, want the default :8080", cfg.Addr)
	}
}
