# moongit CI base image

When CI runs with container isolation (the default — `CODEFORT_CI_ISOLATION=docker`),
moongitd executes each job inside a throwaway container as one streamed
`docker exec <container> provision apply /work/<job>.plan.yml --json` (#411).
So **every image used for CI must carry `provision` (and `git`) on PATH.**
`mooncake` stays too, as long as the `quality` job's own `mooncake task ci`
shell-out is in scope — #411 explicitly excludes rewriting the goq/tq gate
itself as native provision plans; that's a separate, deferred follow-up.
`curl` is needed too: a job using `assert: {http: {...}}` (mooncake's own
shape — provision's `assert` has no HTTP prober) translates to a curl-based
command assert, so any image running one needs `curl` on PATH. This
directory builds the default image, `moongit-ci:latest`.

## Build

1. **Produce a static `mooncake` binary** into `ci/mooncake`. A static build
   (`CGO_ENABLED=0`) runs regardless of the base image's libc:

   ```sh
   # from a mooncake checkout
   CGO_ENABLED=0 go build -o /path/to/moongit/ci/mooncake ./cmd
   ```

   `ci/mooncake` is git-ignored — it's a build input, not source.

2. **Produce a `provision` binary** into `ci/provision`:

   ```sh
   # from a provision checkout
   cargo build --release
   cp target/release/provision /path/to/moongit/ci/provision
   ```

   A plain release build is glibc-linked, which is fine here since
   `moongit-ci:latest` is `debian:stable-slim` (glibc). `ci/provision` is
   git-ignored — it's a build input, not source.

3. **Build the image** from the moongit repo root:

   ```sh
   docker build -t moongit-ci:latest ci/
   ```

The `mooncake --version` / `provision --version` steps in the Dockerfile fail
the build early if either binary is missing or not executable in the image.

## Configuration

The runner reads these settings (see `internal/config/config.go`):

| env | default | meaning |
| --- | --- | --- |
| `CODEFORT_CI_ISOLATION` | `docker` | `docker` runs jobs in containers; `none` runs them on the host (legacy, untrusted). |
| `CODEFORT_CI_DEFAULT_IMAGE` | `moongit-ci:latest` | image a job uses when its `mgitci.yml` doesn't set `image:`. |
| `CODEFORT_CI_JOB_CONCURRENCY` | `4` | how many of a run's jobs run at once; the runner schedules jobs in dependency waves and runs every ready job concurrently up to this cap. |
| `CODEFORT_MAX_CONCURRENCY` | `runtime.NumCPU()` | total runs in flight, CI **and** agent, from one shared budget. Each running job is its own container, so this is the knob that bounds container count. |
| `CODEFORT_AGENT_RESERVED` | `2` | slots inside that budget only agent runs may take, so a CI backlog can never lock out an agent spawn. |

## Triggering runs

A run normally starts on `git push` when an `mgitci.yml` is present at the
pushed commit. Two on-demand paths exist for re-running or starting CI without
a push:

- **Re-run** a past run from the web UI (the Re-run button) or
  `POST /api/repos/{owner}/{repo}/runs/{number}/rerun` — re-enqueues that
  run's exact commit.
- **Manual trigger** for an arbitrary ref (branch, tag, or commit SHA):
  `mgit ci run <ref>`, or `POST /api/repos/{owner}/{repo}/runs` with body
  `{"ref": "<ref>"}`. The server resolves the ref to a commit and enqueues a
  run with event `manual`. CI must be enabled for the repo, and the usual
  `mgitci.yml`-present gate still applies at run time.

## Per-job image override

A job may pin its own image in `mgitci.yml`:

```yaml
version: "1"
jobs:
  build:
    image: my-go-toolchain:latest   # must also carry provision + git on PATH
    steps:
      - run: go build ./...
```

The override image **must include `provision`** (the runner execs it inside
the container). The simplest way is to derive from this base:

```dockerfile
FROM moongit-ci:latest
RUN apt-get update && apt-get install -y --no-install-recommends golang && rm -rf /var/lib/apt/lists/*
```

Scope of the base image is isolation only — it deliberately bundles no language
toolchains. Add what your jobs need in a derived image.

`ci/Dockerfile.dev` builds one such image, `moongit-ci-dev:latest` (base + Go +
Node), which moongit's own `mgitci.yml` runs on:

```sh
docker build -t moongit-ci-dev:latest -f ci/Dockerfile.dev ci/
```

Because forgetting `image:` on a toolchain job only fails at run time
(`go: not found`), `mgit ci validate` warns up front when a job runs a known
toolchain (go, npm, …) but pins no `image:`.

## Runtime notes

- Containers are named `moongit-ci-<jobID>-<job>` and started detached
  (`sleep infinity`); each is removed (`docker rm -f`) when its job finishes.
- A container runs as the moongitd `uid:gid` (`docker run --user`) with the
  checked-out workspace bind-mounted at `/work`, so files it writes stay owned
  by moongitd and workspace cleanup works.
- On startup the runner sweeps any leftover `moongit-ci-*` containers from a
  prior crash.
- Resource/network limits are not yet applied (tracked in moongit issue #26).
