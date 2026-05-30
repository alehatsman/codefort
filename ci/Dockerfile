# moongit CI base image.
#
# moongitd runs each CI job in a throwaway container and execs `mooncake step`
# inside it (see cmd/moongitd/ci_runner.go), so every image used for CI must
# carry `mooncake` and `git` on PATH. This is the default image
# (MOONGIT_CI_DEFAULT_IMAGE=moongit-ci:latest); per-job `image:` overrides in
# mgitci.yml should derive from it (FROM moongit-ci:latest) or bundle mooncake
# the same way.
#
# Scope is isolation only — no language toolchains are baked in. A repo that
# needs Go/node/etc. authors a per-job `image:` pointing at a toolchain image
# that also carries mooncake.
#
# Build (from the moongit repo root):
#   1. produce a static mooncake binary into ci/mooncake (see ci/README.md)
#   2. docker build -t moongit-ci:latest ci/
FROM debian:stable-slim

RUN apt-get update \
 && apt-get install -y --no-install-recommends git ca-certificates \
 && rm -rf /var/lib/apt/lists/*

# mooncake must be a statically-linked build (CGO_ENABLED=0) so it runs
# regardless of the base image's libc. See ci/README.md.
COPY mooncake /usr/local/bin/mooncake
RUN chmod +x /usr/local/bin/mooncake && mooncake --version

# Jobs run as the moongitd uid:gid (injected via `docker run --user` at exec
# time) with the checked-out workspace bind-mounted at /work.
WORKDIR /work
