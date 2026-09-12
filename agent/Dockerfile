# codefort agent base image.
#
# An agent run (the "Spawn agent" button / `cf`) executes a containerized
# Claude session to work an issue — see cmd/codefortd/agent_runner.go. codefortd
# opens this image exactly like a CI container (`docker run --user <uid:gid>
# -v <workspace>:/work … sleep infinity`, then `docker exec`), so it derives
# FROM the CI base to inherit the provision/git contract and the uid:gid
# bind-mount convention, and adds the `claude` CLI plus `cf` on PATH.
#
# This is the default agent image
# (CODEFORT_AGENT_DEFAULT_IMAGE=codefort-agent:latest, parallel to
# CODEFORT_CI_DEFAULT_IMAGE). It stays toolchain-light like the CI base; a repo
# whose agent needs Go/node/etc. derives its own image FROM codefort-agent.
#
# Build (from the codefort repo root) — see agent/README.md:
#   1. drop a static `cf` binary into agent/cf
#   2. docker build -t codefort-agent:latest agent/
FROM codefort-ci:latest

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

# cf — the codefort issue client, so the agent can comment on the issue it's
# working (CODEFORT_TOKEN + CODEFORT_SERVER are injected per run, #77). A static
# build dropped in like ci/provision — a build input, not source
# (agent/cf is git-ignored).
COPY cf /usr/local/bin/cf
RUN chmod +x /usr/local/bin/cf

# The agent runs as an arbitrary uid:gid with no passwd entry, so point HOME at
# a world-writable dir for claude's config/cache/logs (auth itself comes from
# CLAUDE_CODE_OAUTH_TOKEN in the env, not a config file). /tmp is ephemeral,
# which is fine — the container is per-run.
ENV HOME=/tmp

# Fail the build early if any tool the agent needs is missing or unrunnable
# (mirrors the CI base's `provision --version` check).
RUN claude --version && cf help >/dev/null && git --version

WORKDIR /work
