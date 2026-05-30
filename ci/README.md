# moongit CI base image

When CI runs with container isolation (the default — `MOONGIT_CI_ISOLATION=docker`),
moongitd executes each job inside a throwaway container and runs every step as
`docker exec <container> mooncake step '<yaml>'`. So **every image used for CI
must carry `mooncake` (and `git`) on PATH.** This directory builds the default
image, `moongit-ci:latest`.

## Build

1. **Produce a static `mooncake` binary** into `ci/mooncake`. A static build
   (`CGO_ENABLED=0`) runs regardless of the base image's libc:

   ```sh
   # from a mooncake checkout
   CGO_ENABLED=0 go build -o /path/to/moongit/ci/mooncake ./cmd
   ```

   `ci/mooncake` is git-ignored — it's a build input, not source.

2. **Build the image** from the moongit repo root:

   ```sh
   docker build -t moongit-ci:latest ci/
   ```

The `mooncake --version` step in the Dockerfile fails the build early if the
binary is missing or not executable in the image.

## Configuration

The runner reads two settings (see `internal/config/config.go`):

| env | default | meaning |
| --- | --- | --- |
| `MOONGIT_CI_ISOLATION` | `docker` | `docker` runs jobs in containers; `none` runs them on the host (legacy, untrusted). |
| `MOONGIT_CI_DEFAULT_IMAGE` | `moongit-ci:latest` | image a job uses when its `mgitci.yml` doesn't set `image:`. |

## Per-job image override

A job may pin its own image in `mgitci.yml`:

```yaml
version: "1"
jobs:
  build:
    image: my-go-toolchain:latest   # must also carry mooncake + git on PATH
    steps:
      - run: go build ./...
```

The override image **must include `mooncake`** (the runner execs it inside the
container). The simplest way is to derive from this base:

```dockerfile
FROM moongit-ci:latest
RUN apt-get update && apt-get install -y --no-install-recommends golang && rm -rf /var/lib/apt/lists/*
```

Scope of the base image is isolation only — it deliberately bundles no language
toolchains. Add what your jobs need in a derived image.

## Runtime notes

- Containers are named `moongit-ci-<jobID>-<job>` and started detached
  (`sleep infinity`); each is removed (`docker rm -f`) when its job finishes.
- A container runs as the moongitd `uid:gid` (`docker run --user`) with the
  checked-out workspace bind-mounted at `/work`, so files it writes stay owned
  by moongitd and workspace cleanup works.
- On startup the runner sweeps any leftover `moongit-ci-*` containers from a
  prior crash.
- Resource/network limits are not yet applied (tracked in moongit issue #26).
