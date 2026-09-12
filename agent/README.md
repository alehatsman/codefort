# codefort agent base image

An **agent run** (the "Spawn agent" button on an issue) executes a containerized
Claude session to work the issue. codefortd opens this image exactly like a CI
container — `docker run --user <uid:gid> -v <workspace>:/work … sleep infinity`,
then `docker exec` (see `cmd/codefortd/agent_runner.go`) — so it derives `FROM
codefort-ci:latest` to inherit the `provision`/`git` contract and the uid:gid
bind-mount convention, and adds the `claude` CLI plus `cf` on PATH. This
directory builds the default agent image, `codefort-agent:latest`.

## Build

> Shortcut: `provision apply tasks/agent-image.yml` automates everything below
> (it compiles the build inputs and runs the `docker build`). It needs
> `codefort-ci:latest` first — `provision apply tasks/ci-images.yml` builds that.
> The manual steps follow for reference / one-off builds.

1. **Drop a static `cf` binary** into `agent/cf` — a git-ignored build
   input, not source:

   ```sh
   # from the codefort repo root
   CGO_ENABLED=0 go build -o agent/cf ./cmd/cf
   ```

2. **Build the image** from the codefort repo root, once `codefort-ci:latest`
   exists (see `ci/README.md`):

   ```sh
   docker build -t codefort-agent:latest agent/
   ```

The final `claude --version && cf help && git --version` step fails the build
early if any tool is missing or not runnable in the image.

## Configuration

The runner reads these (see `internal/config/config.go`):

| env | default | meaning |
| --- | --- | --- |
| `CODEFORT_AGENT_DEFAULT_IMAGE` | `codefort-agent:latest` | image an agent run executes in (parallel to `CODEFORT_CI_DEFAULT_IMAGE`). |
| `CODEFORT_MAX_CONCURRENCY` | `runtime.NumCPU()` | total runs in flight, agent **and** CI, from one shared budget (not separate pools). |
| `CODEFORT_AGENT_RESERVED` | `2` | slots inside that budget only agent runs may take, so a CI backlog can never starve a spawn. |
| `CODEFORT_AGENT_SERVER_URL` | loopback `CODEFORT_ADDR` | base URL injected as `CODEFORT_SERVER` so in-container `cf` reaches this daemon. |
| `CODEFORT_AGENT_RUN_TIMEOUT` | `60m` | whole-session lifetime cap; a parked run past this is reaped. |
| `CODEFORT_AGENT_TURN_TIMEOUT` | `15m` | per-turn wall-clock limit. |
| `CODEFORT_AGENT_CLAUDE_OAUTH_TOKEN` | — | subscription token (`claude setup-token`) → `CLAUDE_CODE_OAUTH_TOKEN`. |
| `CODEFORT_AGENT_ANTHROPIC_API_KEY` | — | alternate API-key auth → `ANTHROPIC_API_KEY`. |
| `CODEFORT_AGENT_LLM_BASE_URL` | — | optional `ANTHROPIC_BASE_URL` override (a local model later). |

**Settings → Agent overrides (#106, no restart):** the operator can set these in
the UI (persisted in `settings`), and they win over the env per run —
`agent.claude_oauth_token` over `CODEFORT_AGENT_CLAUDE_OAUTH_TOKEN`,
`agent.llm_base_url` over `CODEFORT_AGENT_LLM_BASE_URL` (→ `ANTHROPIC_BASE_URL`).
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
cf). Best for pure code edits.

## What the image carries

Scope is the agent toolchain only — like the CI base, it bundles **no language
toolchains**. A repo whose agent needs Go/node/etc. derives its own image:

```dockerfile
FROM codefort-agent:latest
RUN apt-get update && apt-get install -y --no-install-recommends golang && rm -rf /var/lib/apt/lists/*
```

- `claude` — the Claude Code CLI (runs on the Node runtime installed here).
  Auth comes from `CLAUDE_CODE_OAUTH_TOKEN` / `ANTHROPIC_API_KEY` injected into
  the container env per run (#77), never baked in.
- `cf` — the codefort issue client (`CODEFORT_TOKEN`/`CODEFORT_SERVER` are
  injected per run), exposed to Claude via the MCP shim rather than as a raw
  CLI — Claude itself can't invoke it directly (headless Bash is
  policy-gated, #110). Drop a static `cf` into `agent/cf` (git-ignored
  build input): `CGO_ENABLED=0 go build -o agent/cf ./cmd/cf`.
- `git` — inherited from `codefort-ci:latest`; also used to init the workspace
  repo the agent needs.

## Runtime notes

- The container runs as the codefortd `uid:gid` (`docker run --user`) with no
  passwd entry, so `HOME=/tmp` (world-writable, ephemeral) gives claude a place
  for its config/cache/logs. The workspace is bind-mounted at `/work`.
- It gets `--add-host host.docker.internal:host-gateway` so the in-container
  claude / cf shim can reach the host's codefortd and the LLM endpoint (#77).
- The container is named `codefort-agent-<jobID>`, started detached (`sleep
  infinity`), and **kept alive across turns** (an agent run parks in
  `awaiting_input` between turns). It's removed on finalize; the startup sweep
  reaps leftover `codefort-agent-*` containers from a prior crash.
- Per-run credentials are injected as env at container creation and revoked on
  finalize; the ephemeral codefort token is `agent-run-<runID>`.
- **Force-stop** (`POST /api/repos/{o}/{r}/runs/{n}/cancel`, #146): cancels a
  run from any non-terminal state — unlike Finish, which only accepts a parked
  (`awaiting_input`) run and hands off the work. Cancel interrupts an in-flight
  turn (the runner holds an in-memory cancel handle that unblocks the turn's
  exec), marks the run `canceled`, and **discards** `/work` — no branch is
  materialized. The runner is wired into the API server via `SetAgentCanceler`.
