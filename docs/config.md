# Configuration

`codefortd` is configured entirely through `CODEFORT_*` environment variables —
there is no config file, no flags for these, and nothing persisted at install
time. `internal/config/config.go` (`config.Load`) is the single source of truth:
it reads the environment once at startup, resolves `CODEFORT_DATA_DIR` to an
absolute path, and returns an error that aborts startup if any typed value fails
to parse. A variable set to the **empty string counts as unset** and falls back
to its default, so you cannot blank out a defaulted value by exporting `VAR=`;
to disable something, set the documented disabling value (usually `0`).

Parse failures are loud, not silent: a bad duration (`CODEFORT_CI_RUN_TIMEOUT=15`
— no unit), a non-numeric int/float, or an unknown `CODEFORT_CI_ISOLATION` value
makes `codefortd` exit with `config: CODEFORT_…: …` instead of booting on a
half-understood configuration. Durations use Go syntax (`5s`, `15m`, `168h`).
Range clamping, in contrast, is silent and happens in the runner, not at load —
noted per variable below.

These variables were `MOONGIT_*` before the rename to codefort. The cut is hard
— no fallback read — so a leftover `MOONGIT_ADDR` is not a parse failure; it is
simply not consulted, and `codefortd` boots on that setting's default. Because
that failure mode is silent where every other one here is loud, `codefortd`
warns once per surviving `MOONGIT_*` variable at startup, naming the
`CODEFORT_` replacement. If you see one of those lines, the value you think you
set is not the value in effect.

## Core

| Variable | Default | Purpose |
| --- | --- | --- |
| `CODEFORT_ADDR` | `:8080` | Listen address for the one HTTP port that serves `/api/*`, git smart-HTTP, and the SPA. |
| `CODEFORT_DATA_DIR` | `data` | Root of all server state. Resolved to an absolute path at load (relative values are relative to the process CWD, which makes them fragile under systemd — prefer absolute). Created on startup along with the repos dir. |
| `CODEFORT_DB_PATH` | `$CODEFORT_DATA_DIR/moongit.db` | SQLite database file. Split out only if you want the DB on different storage than the repos. The filename is the one thing the moongit → codefort rename deliberately left alone — renaming it would move live data on every existing deployment for no functional gain. Rename the file and set this var if you want it to match. |
| `CODEFORT_REPOS_DIR` | `$CODEFORT_DATA_DIR/repos` | Bare git repositories. |
| `CODEFORT_HOST_DATA_DIR` | value of `CODEFORT_DATA_DIR` | The **host-side** path that corresponds to `CODEFORT_DATA_DIR` when codefortd itself runs in a container. CI and agent runners bind-mount workspaces into sibling containers via the host Docker daemon, which resolves bind sources on the host filesystem — so a containerised codefortd must translate `/data` back to e.g. `/home/user/.local/share/codefort`. Leave unset for non-containerised deployments. |
| `CODEFORT_WEB_DIR` | *(empty)* | Directory holding the built web UI (`web/dist`). When set, codefortd serves it as an SPA with history-API fallback. Empty means API + git only — the UI is opt-in so a headless deployment ships no static surface. |
| `CODEFORT_EVENT_RETAIN` | `10000` | Cap on retained outbound feed events; a periodic reaper prunes older rows so the `events` table stays bounded. Zero or negative disables retention (unbounded growth). |

## Auth & limits

| Variable | Default | Purpose |
| --- | --- | --- |
| `CODEFORT_BASIC_USER` | *(empty)* | HTTP Basic username gating the web UI and git smart-HTTP. Empty disables Basic auth — codefort is open by default and expects to sit on a trusted network or behind a proxy. |
| `CODEFORT_BASIC_PASS` | *(empty)* | Password for the above. |
| `CODEFORT_RATE_LIMIT` | `0` | Per-IP request rate limit in requests/second (float). `0` disables rate limiting. Non-numeric values fail startup. |
| `CODEFORT_CLAIM_LEASE` | `60m` | How long an issue claim stays exclusive before another agent may steal it. A claim older than this is treated as orphaned — the owner crashed or lost context. Owners heartbeat by re-claiming. `0` disables expiry: claims hold until explicitly unclaimed. |
| `CODEFORT_AGENT_TOKEN_TTL` | `168h` (7d) | Idle TTL for per-agent session tokens (`agent#<n>`), measured from last use, or creation if never used. These are minted one-per-spawn and never explicitly revoked, so a reaper sweeps idle ones. `0` disables the sweep. |

The `/api` surface is unaffected by Basic auth — it keeps Bearer-token auth
(`cf_…`) in all cases.

## Git SSH transport

| Variable | Default | Purpose |
| --- | --- | --- |
| `CODEFORT_SSH_ADDR` | *(empty)* | Listen address for the opt-in git SSH transport, e.g. `:2222`. Empty keeps codefortd a single HTTP port; set it to serve git over SSH with publickey auth against registered keys. |
| `CODEFORT_SSH_HOST_KEY` | `$CODEFORT_DATA_DIR/ssh_host_ed25519_key` | Persisted SSH host private key. Generated (ed25519, mode 0600) on first use if absent, so the host identity is stable across restarts and clients don't see key-changed warnings. Only read when `CODEFORT_SSH_ADDR` is set. |

## CI runner

| Variable | Default | Purpose |
| --- | --- | --- |
| `CODEFORT_CI_ISOLATION` | `docker` | How a job's steps execute. `docker` runs each job in a throwaway container so repo-authored commands never touch the host; `none` runs them on the host as the codefortd user — the legacy path, RCE by design, only for deployments that trust every CI-enabled repo. Any other value **fails startup**. |
| `CODEFORT_CI_DEFAULT_IMAGE` | `codefort-ci:latest` | Image a job runs in when its `codefort.yml` sets no `image:`. Must be glibc-based and carry `provision`, `git`, and `curl` on PATH (see `ci/README.md`). Only used when isolation is `docker`. |
| `CODEFORT_CI_RUN_TIMEOUT` | `15m` | Hard wall-clock limit for a single run; the runner executes under a context with this deadline and kills + errors an overrunning run. Zero or negative disables it — not recommended, CI runs untrusted repo code. |
| `CODEFORT_CI_POLL_INTERVAL` | `5s` | How often an idle runner polls for a queued run. Zero or negative falls back to `5s`; anything under `100ms` is floored there (with a warning), since sub-100ms polling only spins SQLite. |
| `CODEFORT_CI_JOB_CONCURRENCY` | `4` | How many of a run's jobs execute at once. The runner schedules jobs in dependency waves and runs every ready job (all `needs` satisfied) concurrently up to this cap. Values `< 1` are silently clamped to `1`. |
| `CODEFORT_MAX_CONCURRENCY` | `runtime.NumCPU()` | Cap on concurrent **runs** across one shared budget that CI and agent runs both draw from. A CI run still bounds its own jobs by `CODEFORT_CI_JOB_CONCURRENCY`, so this is not a strict cap on job containers. Values `< 1` are clamped to `1`. Replaces the former independent CI/agent run caps. |
| `CODEFORT_CI_SECRET` | *(empty)* | Gates the loopback `/internal/ci/events` endpoint the post-receive hook calls. Empty means the server generates a fresh secret per process — sufficient, since it's injected into the hook env at push time and never persisted. Set it only when something outside the process must call that endpoint. |
| `CODEFORT_CI_RETAIN_RUNS` | `50` | How many of a repo's most recent CI runs are kept; a periodic reaper prunes terminal runs beyond this many along with their on-disk event logs, keeping disk and DB bounded. `queued`/`running` runs are never pruned. Zero or negative disables retention. |

## Agent runs

| Variable | Default | Purpose |
| --- | --- | --- |
| `CODEFORT_AGENT_DEFAULT_IMAGE` | `codefort-agent:latest` | Image an agent run executes in — the CI base plus the `claude` CLI and `cf` (see `agent/README.md`). Only used when isolation is `docker`. |
| `CODEFORT_AGENT_RESERVED` | `2` | Slots of `CODEFORT_MAX_CONCURRENCY` only agent-family runs may take, so a CI backlog can never lock out an agent spawn. Silently clamped to `[0, CODEFORT_MAX_CONCURRENCY]`. |
| `CODEFORT_AGENT_RUN_TIMEOUT` | `60m` | Whole-session lifetime cap. A run parked in `awaiting_input` past this age is reaped — container torn down, run finalized — so an abandoned session can't hold a container forever. Zero or negative disables the reaper. |
| `CODEFORT_AGENT_TURN_TIMEOUT` | `15m` | Per-turn wall-clock limit: one `claude` invocation (first turn or follow-up) runs under this deadline; an overrunning turn is killed and errored. Zero or negative disables the per-turn deadline. |
| `CODEFORT_AGENT_CLAUDE_OAUTH_TOKEN` | *(empty)* | Subscription token from `claude setup-token`, injected per run as `CLAUDE_CODE_OAUTH_TOKEN`. |
| `CODEFORT_AGENT_ANTHROPIC_API_KEY` | *(empty)* | Alternate auth path, injected as `ANTHROPIC_API_KEY`. Exactly one credential is needed for the agent to authenticate headlessly. |
| `CODEFORT_AGENT_LLM_BASE_URL` | *(empty)* | Optional `ANTHROPIC_BASE_URL` override — an Anthropic-compatible gateway now, a local model later. |
| `CODEFORT_AGENT_SERVER_URL` | `http://host.docker.internal:<port of CODEFORT_ADDR>` | How the in-container agent reaches this codefortd for `cf` and git. The default routes over the host gateway (the container gets `--add-host host.docker.internal:host-gateway`); override it when codefortd is reachable at a stable address instead. Port falls back to `8080` if `CODEFORT_ADDR` carries none. |

Credentials are **never baked into the image** — they are injected as container
env at creation and revoked on finalize, alongside an ephemeral per-run codefort
token named `agent-run-<runID>`.

Auth precedence inside the container is a single slot, first match wins:
`ANTHROPIC_AUTH_TOKEN` (Settings only) → `CLAUDE_CODE_OAUTH_TOKEN` from Settings
→ `CODEFORT_AGENT_CLAUDE_OAUTH_TOKEN` → `CODEFORT_AGENT_ANTHROPIC_API_KEY`.

## Client (`cf`)

The client reads no `CODEFORT_*` config beyond two variables; everything else it
derives from the checkout's git remotes.

| Variable | Default | Purpose |
| --- | --- | --- |
| `CODEFORT_TOKEN` | *(empty)* | Bearer token (`cf_…`) sent as `Authorization: Bearer`. **The token's name is your identity** — the server stamps author/assignee/claim owner from it — so never share one. Mint with `codefortd token create <name>`. Unset means unauthenticated requests. |
| `CODEFORT_SERVER` | *(derived)* | Overrides the server base URL otherwise derived from the `codefort` remote (preferred, the code mirror) or `origin`. Trailing slash stripped. **Required** when the chosen remote is SSH or scp-form, since those carry no http base URL — `cf` errors out asking for it. |

Owner/repo always come from the remote URL, never from `CODEFORT_SERVER`, so
`cf` must run inside a checkout of the target repo.

## Runtime overrides

Three agent values are also settable in the UI (Settings → Agent) and persisted
in the `settings` table. They are read **per run**, at spawn time, and win over
their env counterparts — so an operator can rotate a credential or repoint the
LLM endpoint with no `codefortd` restart. Secrets are write-only: the API reports
only whether a value is set, never the value.

| Setting key | Overrides | Injected as |
| --- | --- | --- |
| `agent.claude_oauth_token` | `CODEFORT_AGENT_CLAUDE_OAUTH_TOKEN` | `CLAUDE_CODE_OAUTH_TOKEN` |
| `agent.llm_base_url` | `CODEFORT_AGENT_LLM_BASE_URL` | `ANTHROPIC_BASE_URL` |
| `agent.anthropic_auth_token` | *(no env equivalent)* | `ANTHROPIC_AUTH_TOKEN` — the bearer for a custom-endpoint gateway. When set it claims the container's auth slot alone, ahead of every OAuth/API-key path. |

A fourth key, `agent.execution_model`, is not an env var at all: it sets the
default execution model for runs that don't pick one at spawn (`claude-edit` is
the only valid value today; the field stayed pluggable in shape). Clearing any
of these (setting them to `""` over the API) deletes the row and restores the
env fallback.

## Minimal deployments

**(a) Bare API + git.** No UI, no CI, no agents — a headless coordination
backend. Everything else defaults.

```sh
CODEFORT_ADDR=:8080
CODEFORT_DATA_DIR=/var/lib/codefort
```

**(b) UI + CI.** Serve the SPA and run containerised CI. Requires
`codefort-ci:latest` built (see `ci/README.md`) and a reachable Docker daemon.

```sh
CODEFORT_ADDR=:8080
CODEFORT_DATA_DIR=/var/lib/codefort
CODEFORT_WEB_DIR=/opt/codefort/web/dist
CODEFORT_CI_ISOLATION=docker
CODEFORT_CI_DEFAULT_IMAGE=codefort-ci:latest
CODEFORT_MAX_CONCURRENCY=4
CODEFORT_BASIC_USER=ops
CODEFORT_BASIC_PASS=<pass>
```

**(c) UI + CI + agent runs.** Adds the agent image and a credential.
Credentials can equally be set in Settings → Agent instead of the env.

```sh
CODEFORT_ADDR=:8080
CODEFORT_DATA_DIR=/var/lib/codefort
CODEFORT_WEB_DIR=/opt/codefort/web/dist
CODEFORT_CI_DEFAULT_IMAGE=codefort-ci:latest
CODEFORT_AGENT_DEFAULT_IMAGE=codefort-agent:latest
CODEFORT_MAX_CONCURRENCY=6
CODEFORT_AGENT_RESERVED=2
CODEFORT_AGENT_CLAUDE_OAUTH_TOKEN=<claude setup-token value>
CODEFORT_BASIC_USER=ops
CODEFORT_BASIC_PASS=<pass>
```

If codefortd itself runs in a container, add `CODEFORT_HOST_DATA_DIR` to (b) and
(c) — without it the sibling CI/agent containers bind-mount paths that don't
exist on the host.
