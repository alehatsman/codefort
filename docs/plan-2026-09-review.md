# Plan of work — 2026-09 feature review

Execution plan derived from the full feature review of 2026-09-12. It records
what the review found, what is being changed, and in what order. Each phase is
self-contained and lands as its own commit on `main`.

Status legend: `todo` / `doing` / `done` / `dropped`.

## Context

The review compared the shipped surface (66 API routes, 29 migrations, 34.7k Go
LOC, 20k TS LOC) against `VISION.md`, `specs/`, and the docs. Baseline at review
time: `go build ./... && go test ./...` green.

Three classes of gap came out of it:

1. **A half-shipped access-control feature.** Migrations 24/25/26 (#362/#363/
   #364) added user accounts, repo visibility, and repo membership.
   `storage.CanAccessRepo` / `storage.CanWriteRepo` were written but never
   called — only the repos *list* filters. A `private` repo was readable by any
   token via its direct URL and by anyone via `git clone`.
2. **Spec/code contradiction.** `specs/constitution.md` and
   `specs/identity-and-tokens.md` list multi-user accounts and per-resource
   authorization as explicit Non-goals, while the code ships them.
   `VISION.md` already carves out room for them ("coarse-and-optional, not
   fine-grained-and-load-bearing"); the specs were never updated to match.
3. **Documentation drift and holes.** Six shipped subsystems have no spec, the
   web UI has none at all, 10 of 33 env vars are documented, several docs still
   reference the archived `mooncake` tool for tasks that moved to `provision`,
   and there was no roadmap artifact of any kind.

## Phases

### Phase 0 — plan + roadmap  `done`

- `docs/plan-2026-09-review.md` (this file).
- `ROADMAP.md` — the standing roadmap artifact. Until now the de-facto roadmap
  lived in unchecked spec checklists and bare `#NNN` comment references.

### Phase 1 — P0: enforce repo access  `done`

The load-bearing fix. Design: **one gate, not 61 call sites.**

- `withRepoAccess` middleware on the `/api` mux (inside `withAuth`, so the
  identity is on the context). Matches `/api/repos/{owner}/{repo}/…`, resolves
  the repo once, and enforces:
  - read (`GET`/`HEAD`) → `CanAccessRepo`
  - write (everything else) → `CanWriteRepo`
  - no read access → **404, not 403**, so a private repo's existence does not
    leak.
- `withGitRepoAccess` on the git smart-HTTP mux. Git clients authenticate with
  HTTP Basic, so a moongit token is accepted as the Basic *password* (the
  standard token-over-git-HTTP pattern). Public repos keep today's open
  behavior; private repos require an identity that passes `CanAccessRepo`, and
  `git-receive-pack` additionally requires `CanWriteRepo`.
- The default deployment is unchanged: migration 24 defaults every repo to
  `public`, so this is inert until an operator marks a repo private.

### Phase 2 — P0: reconcile the specs with the code  `done`

- `specs/constitution.md` — replace the "no per-user accounts" Non-goal with
  the coarse-membership posture `VISION.md` actually describes.
- `specs/identity-and-tokens.md` — same, plus the token-as-Basic-password git
  path.
- `specs/access-control.md` — **new.** Owns accounts, visibility, membership,
  and the two gates. The other specs defer to it.

### Phase 3 — P0: LICENSE  `done`

`README.md` flagged its own absence. Nothing can be distributed without it.

### Phase 4 — P1: docs an agent needs to maintain this  `done`

- `CLAUDE.md` — add build/test/lint commands and a doc map. An agent landing in
  the repo currently has to guess between `go test ./...`, `mooncake task ci`,
  and `provision apply tasks/…`, and the genuinely good docs (`web/CLAUDE.md`,
  `docs/specs.md`, `specs/`) are undiscoverable from it.
- `AGENTS.md` — non-Claude harnesses read that filename.
- `docs/config.md` — all 33 `MOONGIT_*` vars in one table. `README.md`
  documented 10; the CI and agent vars were scattered across `ci/README.md` and
  `agent/README.md`.
- `docs/architecture.md` — request graph, the two muxes, the single-writer +
  read-pool split, the data model behind 29 migrations.
- `docs/api.md` — the 66 endpoints, which until now existed only as source
  comments.

### Phase 5 — P1: specs for the unspecced subsystems  `done`

Shipped code with zero spec coverage, in descending order of risk:

- `specs/access-control.md` — accounts / visibility / membership (Phase 2).
- `specs/web-ui.md` — 20k LOC, no spec. `specs/specs.md` already defers its
  Specs-tab behavior to a web spec that did not exist.
- `specs/cli.md` — the `mgit` surface. `specs/mcp-server.md` covers MCP only.
- Extend `specs/issues.md` with labels (migration 23, #340).
- Extend `specs/pull-requests.md` with approvals / review state (migration 28,
  #401, `internal/storage/pr_reviews.go`).
- Extend `specs/ci-pipelines.md` with cron-scheduled runs
  (`cmd/moongitd/cron_scheduler.go`, migration 22, #344).

### Phase 6 — cleanup: stale docs and dead build inputs  `done`

- `agent/Dockerfile` still `COPY`s and version-checks `mooncake` for the
  `mooncake-agent` execution model that commit 7ef8fa9 removed. The image build
  fails without an `agent/mooncake` binary that nothing consumes.
  `tasks/agent-image.yml` still builds it.
- `agent/README.md` points at `mooncake task agent-image` / `ci-images`; both
  moved to `tasks/agent-image.yml` / `tasks/ci-images.yml` (provision).
- `README.md` advertises the dex Intel tab, but the route
  (`web/src/App.tsx`) and the nav tab (`web/src/shell/RepoTabs.tsx`) are both
  commented out. The Intel API is live; the UI is dark.
- `README.md` config table → point at `docs/config.md`.
- `test-results/.last-run.json` is a tracked build artifact.

### Phase 7 — defects and drift found while doing the above  `done`

Not planned; surfaced by the reading each phase required, and small enough
to fix in place rather than defer.

- `/research` and `/summaries` redirected to `/:owner/:repo/explore`, a route
  commented out in ce12340, so both landed on the catch-all not-found page. A
  redirect implies somewhere to go; they now share Explore's fate.
- `mgit issue comment`'s usage string advertised a `--author` flag that was
  never registered, so following the usage produced a parse error. `printUsage`
  had also drifted from the parser on labels, `pr close`/`reopen`,
  `mcp --profile`, the `-y` shorthand, and target resolution (it named
  `origin`, while the client tries the `moongit` remote first).
- `web/CLAUDE.md` pointed at a top-bar theme switcher that lives in Settings,
  conflated the github/monokai schemes with the separate light/dark axis, and
  listed `explore` as live while omitting the routed `specs` feature. It is
  read before every web change, so its errors propagate.
- Four files carried pre-existing `gofmt` drift, fixed in its own commit.

## Deliberately not in scope

- **Finishing the mooncake → provision migration.** `mgitci.yml` still execs
  `mooncake task ci` for the Go quality gate, and `ci/Dockerfile` bakes the
  binary in. This is known and deliberately deferred — the unblocker is the
  go-quality → provision rewrite, which lives upstream in
  `alehatsman/go-quality`, not here. Tracked in `docs/ops-provisioning.md`.
- **The spec verify loop** (stamping `last_verified` / `alignment`). Every spec
  is stuck at `status: draft` because of it, but it is a feature build, not a
  review remediation. Carried on `ROADMAP.md`.
- **Shipping or deleting the Intel/Explore tab.** A product call, not a docs
  fix. The review only corrects the README's claim. Carried on `ROADMAP.md`.

## Phase 8 — remove the dex integration  `done`

Added 2026-09-12 by owner decision: dex is a deprecated experiment and is not
part of moongit's forward direction. This supersedes "Intel / Explore: ship it
or cut it" on `ROADMAP.md` — the call is *cut*, and the cut is total, not just
the dark UI tab.

Everything that exists only to talk to a dex server comes out:

- **Backend.** `internal/dex/` (the client), `internal/server/intel.go` and its
  six `/api/repos/{o}/{r}/intel/*` routes, the `dex` field on `Server`, and the
  `Intel*` response types in `internal/api/types.go`.
- **Spec semantic search.** `POST /api/repos/{o}/{r}/specs/search` is a dex
  search with a `specs/` filter over it — it has no non-dex implementation, so
  it goes with the client. The spec *list*, *read*, *write-via-PR*, and the
  deterministic drift classification are untouched; none of them ever called
  dex.
- **Config.** `MOONGIT_DEX_URL`, `MOONGIT_DEX_TOKEN`,
  `MOONGIT_AGENT_DEX_PROJECT`, and the `DEX_*` env the agent container was
  handed.
- **Agent wiring.** The dex MCP server in the generated agent MCP config, and
  the prompt text telling agents to reach for dex tools. `mgit` stays the
  agent's only MCP server.
- **Web.** The `explore` feature (already unrouted), the Intel card on the repo
  overview, the spec search box, the `intel`/`spec-search` query layer, and the
  specs that mock those endpoints.
- **Repo furniture.** `web/.dex/`, the `.dex` / `agent/dex` ignore rules, and
  the dex mentions in `README.md`, `docs/`, and the specs.
- **`specs/code-intel.md`** is deleted; it governs nothing once the above is
  gone. `specs/constitution.md` gains dex to its non-goals so this does not get
  re-proposed.

Deliberately *not* replaced: nothing here grows a hand-rolled substitute.
Substring search over specs would be a new feature, not a removal, and it can
earn its own slot if the gap is ever felt.

## Phase 9 — roadmap execution  `in progress`

Working the ROADMAP "Next" list directly, now that the review remediation is
closed out.

- **Spec verify loop** — turned out to be already built (#218/#219/#220) and
  merely never *run*. The roadmap entry and the specs checklist both claimed it
  was unimplemented; both corrected, and the DB-vs-frontmatter split the reading
  exposed is now documented in `specs/specs.md`. Actually running a pass needs
  a moongitd with agent credentials, which is an ops step, not a build.
- **Server-side rebase (#257)** — done. A third merge method that replays the
  head'''s commits onto base with `merge-tree --merge-base` + `commit-tree`,
  worktree-free, all-or-nothing.
- **Review state gating merge** — asked, answered yes, built as a per-repo
  opt-in (`require_approval`, migration 30, default off). Default-off is the
  part that needed a decision of its own: a default-on gate would have started
  rejecting merges the fleet was already making, on upgrade, with no warning.
- **Branch protection** — done, the last decision-free item on the "Next" list.
  Per-repo glob patterns (migration 31, empty by default) enforced by a
  `pre-receive` hook that refuses deletes and non-fast-forwards; the patterns
  ride in on the push environment so enforcement needs no network and no DB.
  Spec written first: `specs/branch-protection.md`.
- **`core.hooksPath` trap** — found while testing the above. git resolves
  `core.hooksPath` from the *global* config, so a server whose git user sets it
  in `~/.gitconfig` ran those hooks and none of moongit's: CI-on-push was
  silently dead, with no error anywhere. Fixed by pinning the bare repo's own
  `core.hooksPath` next to the hooks; `ci install-hooks` backfills it. This was
  not on any list — it only showed up because a *hard*-failing hook made the
  silence audible.
- **Issue search** — the last unblocked small. Was a single `LIKE %q%` over
  title and body, so a two-word query only matched the words adjacent and in
  order. Now the query splits on whitespace and every term must hit title or
  body, and an unsorted search is ranked by how many terms land in the *title*
  before the newest-first default. No FTS5 table, no shadow index: the ranking
  is a sum of comparisons in the same query, so there is no second store to
  keep in sync. An explicitly requested sort is never reordered.

What remains on the roadmap is either an ops step (run the spec verify loop,
needs a live moongitd with agent credentials), blocked upstream (mooncake →
provision, waiting on the `go-quality` rewrite), or a genuine judgment call
about scope (milestones, which the roadmap already gates on epics proving
insufficient in practice).
