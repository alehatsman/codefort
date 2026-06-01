# moongit agent base image

An **agent run** (the "Spawn agent" button on an issue) executes a containerized
Claude session to work the issue. moongitd opens this image exactly like a CI
container — `docker run --user <uid:gid> -v <workspace>:/work … sleep infinity`,
then `docker exec` (see `cmd/moongitd/agent_runner.go`) — so it derives `FROM
moongit-ci:latest` to inherit the `mooncake`/`git` contract and the uid:gid
bind-mount convention, and adds the `claude` CLI plus the dex stdio→REST MCP
shim on PATH. This directory builds the default agent image,
`moongit-agent:latest`.

## Build

> Shortcut: `mooncake task agent-image` automates everything below (it compiles
> the build inputs and runs the `docker build`). It needs `moongit-ci:latest`
> first — `mooncake task ci-images` builds that. The manual steps follow for
> reference / one-off builds.

1. **Drop a `dex` binary** carrying the MCP shim (`dex mcp --remote`, from
   dex#6) into `agent/dex`. dex pulls in the sqlite-vec cgo bindings, so it must
   be built with CGO and the `sqlite_fts5` tag — the same build dex itself uses
   (a `CGO_ENABLED=0` build no longer compiles: "build constraints exclude all
   Go files in sqlite-vec-go-bindings/cgo"):

   ```sh
   # from a dex checkout
   CGO_ENABLED=1 go build -tags sqlite_fts5 -o /path/to/moongit/agent/dex ./cmd/dex
   ```

   The tag is a compile-time requirement of dex's package graph, not something
   the shim uses (the shim proxies to a remote `dex serve` and opens no local
   index). The resulting binary is dynamically linked but runs in the
   debian-based image as long as the build host's glibc is no newer than the
   image's.

   `agent/dex` is git-ignored — it's a build input, not source. (`agent/mooncake`
   is reserved the same way if a future build wants a newer mooncake than the
   base image carries.)

2. **Build the image** from the moongit repo root, once `moongit-ci:latest`
   exists (see `ci/README.md`):

   ```sh
   docker build -t moongit-agent:latest agent/
   ```

The final `claude --version && dex --version && mgit help && mooncake --version
&& git --version` step fails the build early if any tool is missing or not
runnable in the image.

> **dex#6 dependency.** Until the dex MCP shim (`dex mcp --remote`) ships, an
> ordinary `dex` binary still builds the image and passes the `--version`
> self-check, but the agent's dex MCP server won't connect at runtime. Rebuild
> with a shim-carrying `dex` once dex#6 lands.

## Configuration

The runner reads these (see `internal/config/config.go`):

| env | default | meaning |
| --- | --- | --- |
| `MOONGIT_AGENT_DEFAULT_IMAGE` | `moongit-agent:latest` | image an agent run executes in (parallel to `MOONGIT_CI_DEFAULT_IMAGE`). |
| `MOONGIT_AGENT_RUN_CONCURRENCY` | `1` | how many agent runs execute at once (separate pool from CI). |
| `MOONGIT_AGENT_RUN_TIMEOUT` | `60m` | whole-session lifetime cap; a parked run past this is reaped. |
| `MOONGIT_AGENT_TURN_TIMEOUT` | `15m` | per-turn wall-clock limit. |
| `MOONGIT_AGENT_CLAUDE_OAUTH_TOKEN` | — | subscription token (`claude setup-token`) → `CLAUDE_CODE_OAUTH_TOKEN`. |
| `MOONGIT_AGENT_ANTHROPIC_API_KEY` | — | alternate API-key auth → `ANTHROPIC_API_KEY`. |
| `MOONGIT_AGENT_LLM_BASE_URL` | — | optional `ANTHROPIC_BASE_URL` override (a local model later). |
| `MOONGIT_AGENT_DEX_PROJECT` | — | dex project id the agent's MCP queries (empty omits dex). |
| `MOONGIT_AGENT_PILOT_MAX_ITERATIONS` | `3` | plan→apply iterations cap per `mooncake-pilot` turn (`mooncake pilot run --max-iterations`). Kept low: the pilot re-runs the whole plan on a failure, so a high cap just re-fails a deterministic step. |
| `MOONGIT_AGENT_PILOT_DENY_ACTIONS` | `shell,cmd` | comma-sep mooncake action types the pilot may **not** use (`--deny-action`); deny wins over allow. Set empty to allow shell. |
| `MOONGIT_AGENT_PILOT_ALLOW_ACTIONS` | — | comma-sep allowlist (`--allow-action`); empty = any action not denied. |
| `MOONGIT_AGENT_PILOT_DENY_NETWORK` | `false` | refuse pilot steps that declare network egress (`--deny-network`). |
| `MOONGIT_AGENT_PILOT_MAX_RISK` | `0` | refuse pilot steps over this risk band 1..10 (`--max-risk`); 0 = no cap. |

## Execution models (#110)

An agent run executes via one of two **pluggable execution models**, chosen
per run at spawn (a selector on the “Spawn agent” button; the default is set in
Settings → Agent or `agent.execution_model`). Both run in this same image and
edit `/work`, so the server-side handoff materializes `agent/issue-N` from the
worktree identically — they differ only in *what runs each turn*:

- **`claude-edit`** (default) — runs `claude -p` directly. Claude edits files
  with its file tools. Under subscription auth its **Bash/execution tools are
  policy-gated** and not reliably unlockable headlessly, so it **can't run
  commands** (tests, git, mgit). Best for pure code edits.
- **`mooncake-pilot`** — runs `mooncake pilot run --provider anthropic-cli
  --auto-apply --output-format json`. Claude is used **only as a planner**
  (it emits a mooncake plan as text); **mooncake validates and applies** the
  plan, so shell/git/test actions execute under mooncake's control rather than
  claude's gated tool-use. Consumes mooncake's NDJSON event stream (mooncake
  #48). Iterations are capped by `MOONGIT_AGENT_PILOT_MAX_ITERATIONS`.

  **Policy (#110/#11):** moving execution into mooncake loses the wall Claude's
  managed Bash policy gave for free, so the runner passes a mooncake per-run
  policy (`--deny-action`/`--allow-action`/`--deny-network`/`--max-risk`) that
  mooncake enforces at preflight — a denied step fails the run *before any side
  effect*. The **default denies `shell` and `cmd`** (the agent uses typed
  actions, not a raw shell); set `MOONGIT_AGENT_PILOT_DENY_ACTIONS=` empty to
  opt into shell (e.g. to let the agent run tests). See the env table above.

## What the image carries

Scope is the agent toolchain only — like the CI base, it bundles **no language
toolchains**. A repo whose agent needs Go/node/etc. derives its own image:

```dockerfile
FROM moongit-agent:latest
RUN apt-get update && apt-get install -y --no-install-recommends golang && rm -rf /var/lib/apt/lists/*
```

- `claude` — the Claude Code CLI (runs on the Node runtime installed here).
  Auth comes from `CLAUDE_CODE_OAUTH_TOKEN` / `ANTHROPIC_API_KEY` injected into
  the container env per run (#77), never baked in.
- `dex` — the stdio→REST MCP shim, wired via a generated `--mcp-config` (#77).
- `mgit` — the moongit issue client (`MOONGIT_TOKEN`/`MOONGIT_SERVER` are
  injected per run). Under **`claude-edit`** Claude can't invoke it (headless
  Bash is policy-gated, #110); under **`mooncake-pilot`** a plan can shell out
  to it, since mooncake — not claude — runs the commands. Drop a static `mgit`
  into `agent/mgit` (git-ignored build input):
  `CGO_ENABLED=0 go build -o agent/mgit ./cmd/moongit`.
- `mooncake` — the executor for the `mooncake-pilot` model (`mooncake pilot
  run`). It must carry `--output-format json` (mooncake #48) **and** the
  permissions-as-contract policy flags (`--deny-action` etc., mooncake #11),
  both now on mooncake `main`. Drop a static build into `agent/mooncake`
  (git-ignored build input): from a mooncake checkout on `main`,
  `CGO_ENABLED=0 go build -o agent/mooncake ./cmd`. (The base image's inherited
  mooncake may be older — this COPY overrides it.)
- `git` — inherited from `moongit-ci:latest`; also used to init the workspace
  repo the pilot model needs.

## Runtime notes

- The container runs as the moongitd `uid:gid` (`docker run --user`) with no
  passwd entry, so `HOME=/tmp` (world-writable, ephemeral) gives claude a place
  for its config/cache/logs. The workspace is bind-mounted at `/work`.
- It gets `--add-host host.docker.internal:host-gateway` so the in-container
  claude / mgit / dex shim can reach the host's moongitd, `dex serve`, and the
  LLM endpoint (#77).
- The container is named `moongit-agent-<jobID>`, started detached (`sleep
  infinity`), and **kept alive across turns** (an agent run parks in
  `awaiting_input` between turns). It's removed on finalize; the startup sweep
  reaps leftover `moongit-agent-*` containers from a prior crash.
- Per-run credentials are injected as env at container creation and revoked on
  finalize; the ephemeral moongit token is `agent-run-<runID>`.
