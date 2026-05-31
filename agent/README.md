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

1. **Drop a static `dex` binary** carrying the MCP shim (`dex mcp --remote`,
   from dex#6) into `agent/dex`. The shim only proxies to a remote `dex serve`,
   so it opens no local index — a plain build is fine (no `sqlite_fts5` tag
   needed, unlike the host indexer):

   ```sh
   # from a dex checkout
   CGO_ENABLED=0 go build -o /path/to/moongit/agent/dex ./cmd/dex
   ```

   `agent/dex` is git-ignored — it's a build input, not source. (`agent/mooncake`
   is reserved the same way if a future build wants a newer mooncake than the
   base image carries.)

2. **Build the image** from the moongit repo root, once `moongit-ci:latest`
   exists (see `ci/README.md`):

   ```sh
   docker build -t moongit-agent:latest agent/
   ```

The final `claude --version && dex --version && mooncake --version &&
git --version` step fails the build early if any tool is missing or not
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
- `mooncake`, `git` — inherited from `moongit-ci:latest`.

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
