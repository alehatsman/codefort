# codefort

**A self-hosted git host, issue tracker, and CI runner that fits in your head.**

codefort is one Go binary that serves the API, git smart-HTTP, a CI
runner, and the web UI on a single port — backed by one SQLite file you
can copy with `cp`. No cluster, no managed database, no object store,
no message queue, no build farm. A git host never needed any of that to
let a few people share code and track what needs doing.

```bash
# One process, one port, one file of state.
codefortd serve            # API + git + CI + web SPA on :8080

# A token names your identity; claims are rows; coordination is HTTP.
codefortd token create alice
export CODEFORT_TOKEN=mgt_...

# The client is a thin, scriptable CLI.
cf issue create --title "ship the thing" --body "plan goes here"
cf issue claim 42 --state in_progress
```

Identity is a token, an issue claim is a lock, and the whole data plane
is plain REST. Nothing to learn that isn't already a git or HTTP
concept.

## Who it's for

- **Small trusting teams** — share code and coordinate work on hardware
  you control. codefort assumes a handful of people who already trust
  each other, not an adversarial public internet, and is far simpler
  for it.
- **Solo developers & self-hosters** — the same binary you'd run "in
  production" is the one you run on your laptop. Offline is the default,
  not a degraded mode.
- **AI agent fleets** — every coordination primitive (issues, claims,
  reviews, CI, the event feed) is exposed over REST *and* an MCP server,
  so a fleet of agents can claim work, report progress, run pipelines,
  and even spawn containerized agent runs straight from an issue.

## Quick start

```bash
go install github.com/alehatsman/codefort/cmd/codefortd@latest
go install github.com/alehatsman/codefort/cmd/cf@latest   # the `cf` client

# Mint a token (shown once) and register a repo.
codefortd token create alice
codefortd repo create alice/widgets

# Run the server. Set CODEFORT_WEB_DIR to also serve the built SPA.
CODEFORT_WEB_DIR=web/dist codefortd serve

# Point a checkout's origin at the server and push.
git remote add origin http://localhost:8080/alice/widgets
git push origin main
```

The client reads the target repo from the checkout's `origin` remote
(or `CODEFORT_SERVER`), and the server stamps author/assignee from the
token's name — so claims and comments are attributed without any
account setup.

## What you can do

Every part of the workflow lives in the same process and the same
SQLite file:

| Capability | What it gives you |
|---|---|
| **Git smart-HTTP** | `git clone` / `push` / `pull` over `:8080`, packs streamed straight from bare repos |
| **Git over SSH** *(opt-in)* | A second listener (`CODEFORT_SSH_ADDR`) maps `publickey` → token identity; off by default to keep it one port |
| **Issues + claims** | Create, list, comment, set-state; `claim`/`unclaim` is a row-level lock so a fleet never double-works an issue |
| **Pull requests** | Open, list, show, and `merge` (with `--ff-only`); diff and compare views in the UI |
| **Code review** | Line-anchored review comments on any ref, resolvable/reopenable, surfaced on the Review tab |
| **CI runner** | `codefort.yml` jobs wired into a DAG via `needs:`, each in a throwaway container; in-process, no worker tier |
| **Agent runs** | Spawn a containerized Claude CLI agent from an issue; live transcript over SSE; awaiting-input → finish lifecycle |
| **Event feed** | `GET /api/events` — a DB-backed SSE stream of push / issue / CI / agent events; `cf events` tails it |
| **MCP server** | `cf mcp` serves the toolset over stdio so an agent drives issues, reviews, pipelines, and runs directly |

The web SPA ships as static files served by the same process — Code,
Commits, Issues, Board, Pulls, Pipelines, Review, Specs, Agents, and
Settings tabs, no SSR tier, no hydration tax.

### The `cf` client

```bash
cf issue list --state todo,in_progress      # survey open work
cf issue show 42
cf issue comment 42 --body "checkpoint: tests green"
cf issue set-state 42 done

cf pr create --base main --head feat/x --title "Add x"
cf pr merge 7 --ff-only

cf review create --path internal/api.go --lines 10-24 --body "nit: name this"
cf review resolve 3

cf ci validate            # check ./codefort.yml
cf ci run main            # trigger a run for a ref

cf events --types issue,ci # tail the fleet feed
```

## The three commitments

codefort is a bet that a git host can stay small forever. Every change
is measured against three lines it will not cross:

1. **Absolute minimalism** — one process, one file of state, a `go.mod`
   you can read in one screen. SQLite via `modernc.org/sqlite` (pure Go,
   no CGO). Minimalism is the feature, not a phase to grow out of.
2. **High performance** — instant cold start (no warm-up, no pools to
   fill), native speed, the SPA as static files, and operations that are
   O(what you'd expect) because there are no other services to fan out
   to.
3. **Local first** — your code, issues, and history live in a local data
   directory that works whether or not the internet does. It's a folder
   and a SQLite file: inspect it, back it up with `cp`, move it to a new
   box. No vendor lock, no export ritual.

When a proposed change pulls against minimalism, performance, or
local-first ownership, the default answer is no. See
[VISION.md](VISION.md) for the full rationale and the explicit list of
nos (no microservices, no required external database, no fine-grained
RBAC matrix, no scale codefort isn't built for).

## Comparison

| Capability | codefort | GitHub / GitLab | bare git + scripts |
|---|---|---|---|
| Single-binary install | ✓ | hosted / heavy self-host | n/a |
| One file of state (SQLite) | ✓ `cp` to back up | managed Postgres + object store | n/a |
| Works fully offline, you own the data | ✓ | ✗ (hosted) / partial | ✓ but no UI/tracker |
| Git host + issues + CI + review in one process | ✓ | ✓ (many services) | ✗ |
| In-process CI, no worker tier | ✓ DAG via `needs:` | ✗ runner fleet | ✗ |
| Claim-as-lock coordination for fleets | ✓ | partial (assignees) | ✗ |
| Agent-native: REST + MCP + spawn-from-issue | ✓ | ✗ | ✗ |
| Fine-grained RBAC matrix | ✗ (by design) | ✓ | n/a |

codefort isn't trying to replace GitHub at organization scale — it ships
the coordination primitives a small trusting team actually needs while
staying a single binary you fully own.

## Configuration

`codefortd` is configured entirely through the environment:

| Variable | Purpose |
|---|---|
| `CODEFORT_ADDR` | Listen address (default `:8080`) |
| `CODEFORT_DATA_DIR` | Data dir for SQLite + repos (default `data`) |
| `CODEFORT_DB_PATH` | SQLite path (default `$CODEFORT_DATA_DIR/moongit.db`) |
| `CODEFORT_REPOS_DIR` | Bare repo root (default `$CODEFORT_DATA_DIR/repos`) |
| `CODEFORT_WEB_DIR` | Built web UI dir (`web/dist`); empty serves API + git only |
| `CODEFORT_BASIC_USER` / `CODEFORT_BASIC_PASS` | Optional HTTP Basic gate on the UI + git |
| `CODEFORT_SSH_ADDR` | Opt-in git SSH transport (e.g. `:2222`); empty keeps it one port |
| `CODEFORT_SSH_HOST_KEY` | SSH host key path (default `$CODEFORT_DATA_DIR/ssh_host_ed25519_key`); generated if absent |

That's the short list — the ones you'll actually set. See
[`docs/config.md`](docs/config.md) for the complete `CODEFORT_*` reference.

The client honors `CODEFORT_TOKEN` (identity) and `CODEFORT_SERVER`
(overrides the `origin` remote when pointing at a specific server).

## Development

```bash
git clone https://github.com/alehatsman/codefort.git
cd codefort

go run ./cmd/codefortd serve        # run the server
go test ./...                      # Go tests

cd web && npm ci && npm run build  # build the SPA into web/dist
```

CI is defined in [`codefort.yml`](codefort.yml) and dogfoods codefort's own
runner: a `quality` Go gate and a `web` build, each in a throwaway
container.

## License

MIT — see [`LICENSE`](LICENSE). Copyright (c) 2026 Aleh Atsman.
