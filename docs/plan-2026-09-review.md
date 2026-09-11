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
