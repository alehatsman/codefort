# moongit — roadmap

What's shipped, what's next, and what we've decided not to build. Measured
against the three commitments in [VISION.md](VISION.md): absolute minimalism,
high performance, local first. A line item that pulls against one of those is
either reshaped until it doesn't, or dropped.

Work is tracked as moongit issues in this repo's own tracker (see
[CLAUDE.md](CLAUDE.md)); this file is the altitude above them — the shape of
the thing, not the task list.

## Shipped

| Capability | Spec |
|---|---|
| Git smart-HTTP: clone / push / fetch, tree, blob, raw, commits, compare | [git-hosting](specs/git-hosting.md) |
| Git over SSH, opt-in, publickey → token identity | [ssh-transport](specs/ssh-transport.md) |
| Issues: CRUD, four-state lifecycle, claim-as-lock with lease, comments, labels | [issues](specs/issues.md) |
| Epics (parent edge) + `depends-on` graph with computed ready/blocked views | [issues](specs/issues.md) |
| Pull requests: open, merge (ff-only / merge-commit / rebase), CAS ref guard, mirror push | [pull-requests](specs/pull-requests.md) |
| PR approvals / review state, opt-in as a merge gate; line-anchored review comments, resolve/reopen | [pull-requests](specs/pull-requests.md) |
| CI: `mgitci.yml` DAG via `needs:`, container isolation, dependency waves, SSE logs, retention | [ci-pipelines](specs/ci-pipelines.md) |
| Cron-scheduled pipeline runs | [ci-pipelines](specs/ci-pipelines.md) |
| Agent runs: spawn-from-issue, turns, park/resume, handoff branch, cancel | [agent-runs](specs/agent-runs.md) |
| Event feed — DB-backed SSE, `mgit events` | [events-feed](specs/events-feed.md) |
| MCP server — 22 tools over stdio | [mcp-server](specs/mcp-server.md) |
| In-repo specs: list, read, write-via-PR, deterministic drift | [specs](specs/specs.md) |
| Accounts, repo visibility, membership, and the two access gates | [access-control](specs/access-control.md) |
| Branch protection: per-repo glob patterns, refusing deletes and force-pushes at push time | [branch-protection](specs/branch-protection.md) |
| Web SPA: Code, Commits, Issues, Board, Pulls, Pipelines, Review, Specs, Agents, Settings | [web-ui](specs/web-ui.md) |
| `mgit` client | [cli](specs/cli.md) |

## Next

Ordered by what unblocks the most.

### 1. Run the spec verify loop (it is built, it has never been used)

The machinery is complete and shipped: the deterministic drift backstop
(#218), the spec-verify agent run (#219), and the pass that parses the agent's
verdict, records it with per-line markers, and stamps `last_verified` /
`alignment` onto a `spec-verify/<id>` branch (#220). What is missing is
*execution* — no verify pass has ever run against this repo, so the
`verifications` table is empty, every spec reads `unverified`, and every spec
except the constitution still says `status: draft`.

That makes this an operations step, not a build: point a moongitd with agent
credentials at this repo, verify each spec, review the stamp branches, and
merge the ones that hold. Only then does a spec earn `living`.

The one code-shaped gap it exposes: a stamp lands on a branch and waits for a
human to merge it, while the drift classifier reads its baseline from the
`verifications` table. The two can therefore disagree — the DB says a spec was
verified at commit X while the spec's own frontmatter on `main` says nothing.
That is defensible (the frontmatter is the human-facing record, the table is
the machine's) but it is undocumented, and it is why "every spec is draft"
reads as a broken feature rather than an unused one.

### 2. Finish mooncake → provision

The last tie to the archived tool is the Go quality gate: `mgitci.yml` execs
`mooncake task ci`, and `ci/Dockerfile` bakes the binary into
`moongit-ci:latest`. The unblocker is upstream — the go-quality → provision
rewrite in `alehatsman/go-quality`. Background in
[docs/ops-provisioning.md](docs/ops-provisioning.md).

### 3. Smaller, unblocked

- Issue search is substring-only over title/body; no ranking.
- No milestones. Labels + epics cover most of what they'd do — this only earns
  a slot if the epic rollup proves insufficient in practice.

## Not building

Restating [VISION.md](VISION.md)'s nos, in roadmap terms, so they stop getting
re-proposed:

- **A fine-grained RBAC matrix.** Coarse membership (owner / write / read) is
  the ceiling. The *grid* of per-user, per-resource rules is out.
- **A public multi-tenant forge.** No forks, no abuse controls, no org
  hierarchy. moongit hosts a known fleet's repos on a trusted box.
- **Outbound webhooks.** The pull-based SSE feed is the chosen shape; moongit
  makes no outbound calls, so there is nothing to configure or fail delivering.
- **Semantic code intelligence.** The dex integration (Intel/Explore, semantic
  spec search, the agent's dex MCP) was an experiment and was removed in full.
  Embedding indexes and meaning-based search are a separate tool's job, not
  moongit's.
- **A worker tier, an external database, or a microservice split.** One
  process, one SQLite file. This is the feature, not a phase.
- **Horizontal scale, replication, failover.** Single-writer, one box. Any such
  effort is a separate, explicitly-flagged design — not an increment on this.
