# moongit agent base image.
#
# An agent run (the "Spawn agent" button / `mgit`) executes a containerized
# Claude session to work an issue — see cmd/moongitd/agent_runner.go. moongitd
# opens this image exactly like a CI container (`docker run --user <uid:gid>
# -v <workspace>:/work … sleep infinity`, then `docker exec`), so it derives
# FROM the CI base to inherit the mooncake/git contract and the uid:gid
# bind-mount convention, and adds the `claude` CLI plus the dex stdio->REST MCP
# shim on PATH.
#
# This is the default agent image
# (MOONGIT_AGENT_DEFAULT_IMAGE=moongit-agent:latest, parallel to
# MOONGIT_CI_DEFAULT_IMAGE). It stays toolchain-light like the CI base; a repo
# whose agent needs Go/node/etc. derives its own image FROM moongit-agent.
#
# Build (from the moongit repo root) — see agent/README.md:
#   1. drop a static `dex` binary carrying `dex mcp --remote` into agent/dex
#   2. docker build -t moongit-agent:latest agent/
FROM moongit-ci:latest

# The Claude CLI runs on Node. Install Node 20 (NodeSource) + the CLI globally
# so `claude` is on PATH for any uid; build-only tools are removed after.
RUN apt-get update \
 && apt-get install -y --no-install-recommends curl ca-certificates gnupg \
 && curl -fsSL https://deb.nodesource.com/setup_20.x | bash - \
 && apt-get install -y --no-install-recommends nodejs \
 && npm install -g @anthropic-ai/claude-code \
 && npm cache clean --force \
 && apt-get purge -y curl gnupg \
 && apt-get autoremove -y \
 && rm -rf /var/lib/apt/lists/*

# dex stdio->REST MCP shim (option A): a static `dex` binary carrying
# `dex mcp --remote <url>` (dex#6). Dropped in like ci/mooncake — a build
# input, not source (agent/dex is git-ignored). The shim only proxies to a
# remote `dex serve`, so it opens no local index (no FTS5 build tag needed,
# unlike the host indexer).
COPY dex /usr/local/bin/dex
RUN chmod +x /usr/local/bin/dex

# mgit — the moongit issue client, so the agent can comment on the issue it's
# working (MOONGIT_TOKEN + MOONGIT_SERVER are injected per run, #77). A static
# build dropped in like dex/mooncake (agent/mgit is a git-ignored build input).
COPY mgit /usr/local/bin/mgit
RUN chmod +x /usr/local/bin/mgit

# The agent runs as an arbitrary uid:gid with no passwd entry, so point HOME at
# a world-writable dir for claude's config/cache/logs (auth itself comes from
# CLAUDE_CODE_OAUTH_TOKEN in the env, not a config file). /tmp is ephemeral,
# which is fine — the container is per-run.
ENV HOME=/tmp

# Fail the build early if any tool the agent needs is missing or unrunnable
# (mirrors the CI base's `mooncake --version` check).
RUN claude --version && dex --version && mgit help >/dev/null && mooncake --version && git --version

WORKDIR /work
