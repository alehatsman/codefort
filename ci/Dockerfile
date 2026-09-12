# moongit CI base image.
#
# moongitd runs each CI job in a throwaway container and execs `provision
# apply <plan> --json` inside it (see cmd/codefortd/ci_runner.go, #411), so
# every image used for CI must carry `provision` and `git` on PATH. `mooncake`
# stays too, as long as the `quality` job's `mooncake task ci` shell-out is in
# scope (#411 explicitly excludes the goq/tq rewrite — that's a separate,
# larger, deferred follow-up). This is the default image
# (CODEFORT_CI_DEFAULT_IMAGE=moongit-ci:latest); per-job `image:` overrides in
# mgitci.yml should derive from it (FROM moongit-ci:latest) or bundle both
# binaries the same way.
#
# `curl` is baked in too: mooncake's `assert: {http: {...}}` (no provision
# equivalent — assert takes command/expr only) translates to a curl-based
# command assert (docs/ops-provisioning.md's CI-runner section). mooncake's
# own http assert used its built-in Go HTTP client and needed no external
# binary; provision's does, so this is a new image dependency the swap
# introduces — found by actually running the `smoke` job's translated plan
# for real (curl: command not found on the untouched base image).
#
# Scope is otherwise isolation only — no language toolchains are baked in. A
# repo that needs Go/node/etc. authors a per-job `image:` pointing at a
# toolchain image that also carries provision.
#
# Build (from the moongit repo root):
#   1. produce a static mooncake binary into ci/mooncake (see ci/README.md)
#   2. produce a provision binary into ci/provision (see ci/README.md)
#   3. docker build -t moongit-ci:latest ci/
FROM debian:stable-slim

RUN apt-get update \
 && apt-get install -y --no-install-recommends git ca-certificates curl \
 && rm -rf /var/lib/apt/lists/*

# mooncake must be a statically-linked build (CGO_ENABLED=0) so it runs
# regardless of the base image's libc. See ci/README.md.
COPY mooncake /usr/local/bin/mooncake
RUN chmod +x /usr/local/bin/mooncake && mooncake --version

# provision is glibc-linked (a plain `cargo build --release`) — fine here
# since debian:stable-slim is glibc-based. See ci/README.md.
COPY provision /usr/local/bin/provision
RUN chmod +x /usr/local/bin/provision && provision --version

# Jobs run as the moongitd uid:gid (injected via `docker run --user` at exec
# time) with the checked-out workspace bind-mounted at /work.
WORKDIR /work
