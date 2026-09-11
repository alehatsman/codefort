# moongit agent base image

An **agent run** (the "Spawn agent" button on an issue) executes a containerized
Claude session to work the issue. moongitd opens this image exactly like a CI
container — `docker run --user <uid:gid> -v <workspace>:/work … sleep infinity`,
then `docker exec` (see `cmd/moongitd/agent_runner.go`) — so it derives `FROM
moongit-ci:latest` to inherit the `provision`/`git` contract and the uid:gid
bind-mount convention, and adds the `claude` CLI plus the dex stdio→REST MCP
shim on PATH. This directory builds the default agent image,
`moongit-agent:latest`.

## Build

> Shortcut: `provision apply tasks/agent-image.yml` automates everything below
> (it compiles the build inputs and runs the `docker build`). It needs
> `moongit-ci:latest` first — `provision apply tasks/ci-images.yml` builds that.
> The manual steps follow for reference / one-off builds.

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

   `agent/dex` is git-ignored — it's a build input, not source.

2. **Build the image** from the moongit repo root, once `moongit-ci:latest`
   exists (see `ci/README.md`):

   ```sh
   docker build -t moongit-agent:latest agent/
   ```

The final `claude --version && dex --version && mgit help && git --version`
step fails the build early if any tool is missing or not runnable in the
image.

> **dex#6 dependency.** Until the dex MCP shim (`dex mcp --remote`) ships, an
> ordinary `dex` binary still builds the image and passes the `--version`
> self-check, but the agent's dex MCP server won't connect at runtime. Rebuild
> with a shim-carrying `dex` once dex#6 lands.

## Configuration

The runner reads these (see `internal/config/config.go`):

| env | default | meaning |
| --- | --- | --- |
| `MOONGIT_AGENT_DEFAULT_IMAGE` | `moongit-agent:latest` | image an agent run executes in (parallel to `MOONGIT_CI_DEFAULT_IMAGE`). |
| `MOONGIT_MAX_CONCURRENCY` | `runtime.NumCPU()` | total runs in flight, agent **and** CI, from one shared budget (not separate pools). |
| `MOONGIT_AGENT_RESERVED` | `2` | slots inside that budget only agent runs may take, so a CI backlog can never starve a spawn. |
| `MOONGIT_AGENT_SERVER_URL` | loopback `MOONGIT_ADDR` | base URL injected as `MOONGIT_SERVER` so in-container `mgit` reaches this daemon. |
| `MOONGIT_AGENT_RUN_TIMEOUT` | `60m` | whole-session lifetime cap; a parked run past this is reaped. |
| `MOONGIT_AGENT_TURN_TIMEOUT` | `15m` | per-turn wall-clock limit. |
| `MOONGIT_AGENT_CLAUDE_OAUTH_TOKEN` | — | subscription token (`claude setup-token`) → `CLAUDE_CODE_OAUTH_TOKEN`. |
| `MOONGIT_AGENT_ANTHROPIC_API_KEY` | — | alternate API-key auth → `ANTHROPIC_API_KEY`. |
| `MOONGIT_AGENT_LLM_BASE_URL` | — | optional `ANTHROPIC_BASE_URL` override (a local model later). |
| `MOONGIT_AGENT_DEX_PROJECT` | — | dex project id the agent's MCP queries (empty omits dex). |

**Settings → Agent overrides (#106, no restart):** the operator can set these in
the UI (persisted in `settings`), and they win over the env per run —
`agent.claude_oauth_token` over `MOONGIT_AGENT_CLAUDE_OAUTH_TOKEN`,
`agent.llm_base_url` over `MOONGIT_AGENT_LLM_BASE_URL` (→ `ANTHROPIC_BASE_URL`).
A new `agent.anthropic_auth_token` injects `ANTHROPIC_AUTH_TOKEN` (the bearer for
a custom-endpoint gateway); when set it claims the container's auth slot alone,
ahead of the OAuth/API-key paths. Secrets are stored write-only.

## Execution model (#110)

An agent run executes via `claude-edit`, the only execution model (the
default is set in Settings → Agent or `agent.execution_model`; the field
stayed pluggable in shape for a possible future model). It runs `claude -p`
directly inside this image and edits `/work` with its file tools, so the
server-side handoff materializes `agent/issue-N` from the worktree. Under
subscription auth its **Bash/execution tools are policy-gated** and not
reliably unlockable headlessly, so it **can't run commands** (tests, git,
mgit). Best for pure code edits.

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
  injected per run), exposed to Claude via the MCP shim rather than as a raw
  CLI — Claude itself can't invoke it directly (headless Bash is
  policy-gated, #110). Drop a static `mgit` into `agent/mgit` (git-ignored
  build input): `CGO_ENABLED=0 go build -o agent/mgit ./cmd/moongit`.
- `git` — inherited from `moongit-ci:latest`; also used to init the workspace
  repo the agent needs.

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
- **Force-stop** (`POST /api/repos/{o}/{r}/runs/{n}/cancel`, #146): cancels a
  run from any non-terminal state — unlike Finish, which only accepts a parked
  (`awaiting_input`) run and hands off the work. Cancel interrupts an in-flight
  turn (the runner holds an in-memory cancel handle that unblocks the turn's
  exec), marks the run `canceled`, and **discards** `/work` — no branch is
  materialized. The runner is wired into the API server via `SetAgentCanceler`.
