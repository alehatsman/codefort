# Architecture — moongit

moongit is one Go process (`moongitd`) that serves the `/api` surface, git
smart-HTTP, the built SPA, and the in-process CI/agent runner on a single port,
against a single SQLite file and a directory of bare repos. Everything else in
this document follows from that: there is no worker tier to schedule against, no
message bus to observe, no second datastore to keep consistent — so the
interesting design pressure lands on *request dispatch*, *one writer*, and
*background loops sharing one budget*. The commitments this serves are stated in
[`VISION.md`](../VISION.md) (one process, one file of state, local-first) and
[`specs/constitution.md`](../specs/constitution.md) (local-trust, git as
substrate, boring mechanisms).

---

## Process layout

`moongitd` is also a small admin CLI; `cmd/moongitd/main.go` dispatches
`serve` (the default with no args), `repo create`, `token create|list|revoke`,
and `ci install-hooks`. Only `serve` starts the system.

`runServe` (`cmd/moongitd/main.go:94`) does, in order:

1. `config.Load()` — every knob is a `MOONGIT_*` env var (`internal/config`).
   `EnsureDirs()` creates the data dir, repos dir.
2. `storage.Open(cfg.DBPath)` — the **writer** pool.
3. `storage.Migrate(db)` — schema catch-up, on the writer, before anything else
   touches the file.
4. `storage.OpenRead(cfg.DBPath)` — the **reader** pool. Opened *after*
   `Migrate` so the file is already in WAL mode.
5. Warn if there are zero active tokens (every `/api/*` request would 401).
6. `server.New(cfg, db, rdb, logger)`, then `newCIRunner(...)` and
   `srv.SetAgentCanceler(runner)` — the runner is created here, not inside its
   goroutine, because the HTTP force-stop endpoint needs the runner's in-memory
   container handles (a DB-only "cancelled" flag cannot interrupt a live
   container).
7. Background goroutines: claim reaper, token reaper, CI retention reaper,
   event retention reaper, cron scheduler, CI/agent runner.
8. HTTP listener; plus the **optional** SSH listener when `MOONGIT_SSH_ADDR` is
   set (`internal/server/ssh.go`) — the one second listener, same process.
9. Signal wait, then the shutdown drain (see *Background work*).

Nothing warms up. Startup cost is opening a file and replaying any unapplied
migrations.

---

## Request graph

`Server.Handler()` (`internal/server/server.go:87`) composes the whole graph.

**Why two muxes.** Git smart-HTTP needs bare-wildcard patterns
(`/{owner}/{repo}/info/refs`). Go's `http.ServeMux` refuses to let those coexist
with `/api/...` on the same mux — the patterns conflict. So there are two
`ServeMux` instances and a hand-written top-level prefix dispatcher in front of
them. The dispatcher earns its keep twice: it separates the pattern namespaces,
*and* it scopes auth cleanly (bearer auth wraps only the API mux; basic auth
wraps only the git + web surfaces).

```
                        withLogging                (statusRecorder; re-exposes Flush for SSE)
                             │
                     root dispatcher  (plain switch on r.URL.Path)
                             │
  ┌──────────┬───────────────┼─────────────────┬──────────────┬─────────────┐
  │          │               │                 │              │             │
/healthz  /internal/    /api/auth/*         /api/*        isGitRequest(r)   else
  │        ci/events         │                 │                 │           │
  │          │               │                 │                 │           │
handleHealth │          withRateLimit    withRateLimit       withBasicAuth  withBasicAuth
(pings the   │               │                 │                 │           │
 WRITER pool)│          publicAPIHandler   withAuth        withGitRepoAccess  webHandler
             │           (register,           │                 │        (SPA, index
        handleCIEvents     login)         withRepoAccess      gitHandler   fallback)
        loopback-only +                        │              (ServeMux,       │
        CI-secret gated                    apiHandler          bare        nil when
                                           (ServeMux,          wildcards)  MOONGIT_WEB_DIR
                                            /api/... )                     unset → git mux
```

Dispatch order is exact and matters — it is a `switch` in
`internal/server/server.go:106-129`:

| # | Match | Handler | Auth |
|---|---|---|---|
| 1 | `path == "/healthz"` | `handleHealth` | none |
| 2 | `path == "/internal/ci/events"` | `handleCIEvents` | loopback check + constant-time CI-secret header |
| 3 | `strings.HasPrefix(path, "/api/auth/")` | `publicAPIHandler` | rate limit only |
| 4 | `strings.HasPrefix(path, "/api/")` | `apiHandler` | rate limit → bearer → repo access |
| 5 | `isGitRequest(r)` | `gitHandler` | basic auth (if configured) → repo access |
| 6 | web enabled | SPA | basic auth (if configured) |
| 7 | default | `gitHandler` (404s) | basic auth |

Notes that bite if you get them wrong:

- **`/api/auth/` must be tested before `/api/`** — reordering those two silently
  makes register/login require the token they exist to mint.
- **`isGitRequest`** (`internal/server/web.go:42`) matches by *suffix*
  (`/info/refs`, `/git-upload-pack`, `/git-receive-pack`), not by prefix. That
  is what lets `git clone http://host/owner/repo` and a browser visiting
  `/owner/repo` share a URL space: the browser path falls through to the SPA.
- **`/healthz` pings `s.db`, the writer** (`server.go:253`), not the reader. A
  bare 200 would read false-green to a supervisor while the single writer is
  wedged — which is exactly the failure that matters. Bounded by a 2s timeout.
- **Rate limiting** (`internal/server/ratelimit.go`) is a stdlib-only per-IP
  token bucket, applied *before* auth so failed auth attempts are throttled too.
  `nil` (disabled) unless `MOONGIT_RATE_LIMIT > 0`; burst is `3 × rate`.
- **Bearer auth** (`internal/server/auth.go:37`) looks the token up on the
  **reader**, and writes `last_used_at` on the **writer** — debounced to
  `tokenTouchInterval` (5 min) so a polling agent fleet doesn't turn every read
  into a serialized write.
- **Basic auth** (`auth.go:72`) is a single optional credential
  (`MOONGIT_BASIC_USER`/`_PASS`) gating the human/git surfaces; constant-time
  compare; no-op when unset. It never wraps `/api`, which keeps its own bearer
  auth, and never wraps `/healthz`.
- **Repo access** (`internal/server/access.go`) is enforced in exactly **two
  middlewares**, never at the ~60 handler call sites that resolve a repo — one
  gate is auditable, sixty are not, and the first cut of this feature shipped
  with the check functions written but never called. `withRepoAccess` sits
  *inside* `withAuth` (so the token is already on the context) and *outside* the
  route mux (so a new route is gated by construction). `withGitRepoAccess` is
  nested inside `withBasicAuth`: the shared deployment credential first, then
  per-repo visibility against the caller's own token. Both are no-ops for
  `public` repos, which is the default — private is opt-in, and the posture
  stays coarse membership (owner / write / read), not an RBAC matrix. A caller
  without read access gets a **404**, not a 403; 403 would confirm the repo
  exists.
- **Identity vs. principal.** `identityFromContext` returns the token *name* and
  is what stamps authorship on every write. Access checks instead use
  `principalFromContext`, which resolves the linked *account* — a browser
  session token is named `<user>-session`, so matching on the raw name would
  lock a user out of their own private repo the moment they signed in. Falls
  back to the token name for tokens with no linked account (admin-provisioned
  and per-run agent tokens). That, plus the claim lock and author-only deletes,
  is the whole authorization story — local-trust, per the constitution.

---

## Storage

`internal/storage/sqlite.go` is 45 lines and holds the most load-bearing
decision in the system: **two pools over one file**.

```go
Open(path)      // journal_mode(WAL), foreign_keys(1), busy_timeout(5000)
                // db.SetMaxOpenConns(1)        ← the single writer
OpenRead(path)  // query_only(1),   foreign_keys(1), busy_timeout(5000)
                // db.SetMaxOpenConns(4)        ← readPoolSize
```

- **`s.db` — writer, exactly one connection.** All mutations go here. The
  single-connection cap is not a performance concession, it is a correctness
  mechanism: the per-repo issue/PR/run number allocator and the claim
  compare-and-swap rely on writes serializing, and it makes `SQLITE_BUSY`
  between our own writes structurally impossible.
- **`s.rdb` — reader, up to 4 connections, `query_only(1)`.** Pure reads go
  here. WAL lets them run concurrently with the writer instead of queueing
  behind it, which is the difference between a responsive UI and a UI that
  stalls behind one slow merge.

**Handlers pick explicitly.** There is no router, no magic, no "the framework
decides": a handler that reads calls `storage.X(s.rdb, …)` and a handler that
writes calls `storage.X(s.db, …)`. Getting it backwards fails in two directions:

- *Read on the writer* — quietly serializes the read behind every in-flight
  mutation. Under a polling fleet this is the classic symptom of "moongit feels
  slow for no reason," and it is invisible in tests.
- *Write on the reader* — `query_only(1)` rejects it, so this fails loudly. That
  pragma is deliberately a defensive backstop, not an assumption.

**Migrations** (`internal/storage/migrations.go`) are an ordered `[]string` of
SQL, applied on the writer at startup by `Migrate(db)` (from `runServe`, and
from `openDB()` for the admin subcommands). Version tracking lives in
`schema_version`; each migration and its version bump run in **one transaction**
(SQLite has transactional DDL), so a multi-statement migration that fails
partway rolls back wholesale rather than leaving a half-applied schema that
can't be re-run. The rule at the top of the file is the rule: **append only,
never edit a committed migration.**

---

## Data model

Derived from `internal/storage/migrations.go` (29 migrations at time of
writing). Key columns only; FKs cascade from `repos`/`users` unless noted.

### Identity & access

| Table | Key columns | Notes |
|---|---|---|
| `users` | `id`, `name` UNIQUE, `password_hash` (nullable) | Admin-provisioned users have no hash and authenticate by token only; registered users carry bcrypt. |
| `tokens` | `id`, `name` UNIQUE, `hashed_token` UNIQUE, `user_id`→`users`, `last_used_at`, `revoked_at` | Plaintext is never stored — only `sha256`. Revocation is a soft `revoked_at`. `user_id` is nullable (agent/legacy tokens). Token **name** is the identity on every write. |
| `ssh_keys` | `id`, `token_id`→`tokens` (CASCADE), `fingerprint` UNIQUE, `public_key`, `last_used_at` | SHA256 fingerprint is the auth lookup key. One identity primitive (the token), two credentials. |
| `repo_members` | `id`, `repo_id`, `user_id`, `role` (`read`\|`write`), UNIQUE(repo,user) | Collaborators only — the owner is **not** a row here; ownership derives from `repos.owner_id`. |

### Repos

| Table | Key columns | Notes |
|---|---|---|
| `repos` | `id`, `owner_id`→`users`, `name`, UNIQUE(owner,name), `ci_enabled` (default 0), `visibility` (`public`\|`private`, default `public`) | CI is strictly opt-in: it runs untrusted repo code. |

### Issues

| Table | Key columns | Notes |
|---|---|---|
| `issues` | `id`, `repo_id`, `number` (per-repo, UNIQUE(repo,number)), `title`, `body`, `author`, `state`, `assignee`, `claimed_at`, `parent_number`, `labels` (JSON array, default `[]`) | Invariant: `claimed_at IS NOT NULL` iff `assignee IS NOT NULL` — the claim lease. `parent_number` is the epic link (per-repo number, no FK). `labels` avoids a join table; query via `json_each()`. |
| `issue_comments` | `id`, `issue_id`→`issues` (CASCADE), `author`, `body` | |
| `issue_dependencies` | `id`, `repo_id`, `issue_number`, `depends_on_number`, UNIQUE triple | Directed "blocked until" edges; a DAG (self-edges and cycles rejected in the storage layer). No FK on the numbers, so `DeleteIssue` clears edges in both directions manually (`internal/storage/dependencies.go`). |

### Pull requests & review

| Table | Key columns | Notes |
|---|---|---|
| `pull_requests` | `id`, `repo_id`, `number` (per-repo), `base_ref`, `head_ref`, `title`, `body`, `author`, `state` (`open`\|`merged`\|`closed`), `merged_at`, `merge_base_sha`, `merge_head_sha` | The two `merge_*_sha` columns freeze the pre-merge compare endpoints; without them a merged PR renders an empty diff (head is contained in base, so `merge-base == head`). |
| `pr_reviews` | `id`, `repo_id`, `pr_number`, `author`, `state` CHECK(`approved`\|`changes_requested`), UNIQUE(repo,pr,author) | Upserted, so a reviewer can change their mind. |
| `code_comments` | `id`, `repo_id`, `ref`, `path`, `start_line`, `end_line`, `commit_sha`, `author`, `body`, `resolved` | Anchored to a *branch* + line range. **PR review threads reuse this table** (anchored to `head_ref`) — there is deliberately no PR-comment table. `commit_sha` freezes the ref tip so a reader can tell if lines drifted. |

### Runs (CI + agent — one spine)

| Table | Key columns | Notes |
|---|---|---|
| `ci_runs` | `id`, `repo_id`, `number` (per-repo), `commit_sha`, `ref`, `event`, `trigger`, `status`, `claimed_at`, `commit_msg`, `commit_author`, `kind` (`ci`\|`agent`\|`spec-verify`), `issue_number`, `spec_path`, `execution_model` (`claude-edit`), `tool_profile` (`full`\|`review`) | One table, three kinds. An agent run is a synthetic single-job run so it inherits the whole CI spine: event log, SSE, container isolation, lifecycle. `claimed_at` is the runner lease, so a crashed runner's `running` row can be reclaimed. `commit_msg`/`commit_author` are frozen at enqueue (a SHA has no human meaning in a list view). |
| `ci_jobs` | `id`, `run_id`→`ci_runs` (CASCADE), `name`, `status`, `exit_code`, `needs` (JSON array of job names) | `needs` lets the run-detail view draw the DAG instead of a flat tab list. |
| `agent_turns` | `id`, `run_id`→`ci_runs` (CASCADE), `seq` (per-run), `author`, `body`, `status`, `claimed_at` | An agent run is a conversation. Turn 1 is the issue body (executed inline, not stored); each later message is a row dispatched as its own `claude --resume` turn while the run parks in `awaiting_input`. |
| `cron_schedules` | `id`, `repo_id`, `cron_expr`, `last_fired_at`, UNIQUE(repo,expr) | Lets the scheduler compute the next due time without scanning `ci_runs`. Reconciled (upsert + delete-stale) against the repo's pipeline on every tick. |

### Specs, feed, config

| Table | Key columns | Notes |
|---|---|---|
| `spec_verifications` | `id`, `repo_id`, `spec_id` (frontmatter id, stable across moves), `spec_path`, `commit_sha`, `alignment` REAL, `result` (JSON), `verifier` | Queryable history behind the at-a-glance result in the spec's frontmatter. |
| `events` | `id` (global monotonic = the SSE event id), `type`, `repo_id` (nullable), `actor`, `payload` (opaque JSON) | Append-only feed replayed + live-tailed by `GET /api/events`; resumable via `Last-Event-ID`. Bounded by the retention reaper. |
| `settings` | `key` PK, `value`, `updated_at` | Operator config that shouldn't need a restart (e.g. the agent Claude token). **Values may be secrets — the API surface must be write-only and never echo one back.** |
| `schema_version` | `version` PK, `applied_at` | Migration bookkeeping. |

---

## Git as substrate

Bare repos on disk are the source of truth; SQLite holds coordination metadata
*about* them, never a copy of history.

- **Layout:** `$MOONGIT_REPOS_DIR/<owner>/<name>.git`, created by
  `git init --bare -b main` (`internal/server/repos.go:130`). `repoPath`
  (`internal/server/git.go:23`) resolves `{owner}/{repo}` and rejects traversal,
  accepting both `name` and `name.git` because the router pattern and real git
  clients disagree on the suffix.
- **Smart-HTTP streams, it does not buffer.** `handleInfoRefs` writes the
  pkt-line service header itself, then hands `w` directly to
  `git upload-pack --stateless-rpc --advertise-refs` as `cmd.Stdout`.
  `handleServiceRPC` wires `cmd.Stdin = body` (transparently gunzipped by
  `decodeBody`) and `cmd.Stdout = w`. There is no worktree, no temp file, and no
  pack held in memory — moongit is a pipe between the client and `git`.
- **Push hooks carry no secrets on disk.** On `git-receive-pack`,
  `handleServiceRPC` injects `MOONGIT_CI_URL`, `MOONGIT_CI_SECRET`,
  `MOONGIT_CI_REPO`, `MOONGIT_CI_PUSHER` into the child's environment; the
  generic `post-receive` hook (`internal/server/ci_hook.go`) inherits them and
  POSTs each pushed ref to the loopback `/internal/ci/events`. The secret is
  per-process and never persisted. The hook soft-fails — a CI problem must never
  block a push.
- **Server-side operations are worktree-free**, because the same bare repo is
  being served concurrently. Reads use plumbing (`cat-file -t/-s/blob`,
  `--git-dir … show`) in `internal/server/tree.go` and
  `internal/server/commits.go`. The **merge** path
  (`internal/server/merge.go:25`) is `merge-tree --write-tree` → `commit-tree` →
  `update-ref <ref> <new> <old>`; the old-OID argument makes the ref move atomic
  against a concurrent push, and the code re-reads the ref to verify it actually
  advanced. Conflicts are reported, never auto-resolved — the author rebases
  locally and pushes.
- **The canonical branch advances through PR merges.** There is no server-side
  "commit to main" path; `main` moves when `update-ref` runs inside
  `handleMergePull` or when someone pushes.
- **The CI checkout is the one place that materializes a tree**, and it does so
  *beside* the bare repo, never in it: `gitCheckout`
  (`cmd/moongitd/ci_runner.go:1308`) does `git clone --local --no-checkout`
  (hardlinked objects, no copy, no network) then a detached checkout of the
  exact SHA. A `git archive | tar` extract would be lighter but leaves no `.git`,
  which breaks quality gates that shell out to `git rev-parse --show-toplevel`.

---

## Background work

All loops take the **writer** pool and all stop on the signal context.

| Loop | Entry | Cadence | Purpose |
|---|---|---|---|
| Claim reaper | `runClaimReaper` (`main.go:229`) | `lease/2`, floor 1 min | Releases expired issue claims so orphaned work is *discoverable*, not merely stealable. Disabled when `ClaimLease <= 0`. |
| Token reaper | `runTokenReaper` (`main.go:264`) | `ttl/2`, floor 1 min | Revokes idle per-agent session tokens (`agent#<n>`), which the launcher mints per spawn and never cleans up. |
| CI retention | `runCIRetentionReaper` (`main.go:302`) | 10 min, plus once at startup | Prunes terminal runs beyond `CIRetainRuns` and deletes their on-disk event logs. |
| Event retention | `runEventRetentionReaper` (`main.go:359`) | 10 min, plus once at startup | Bounds the `events` feed to `EventRetain` rows. |
| Cron scheduler | `runCronScheduler` (`cron_scheduler.go:24`) | 30 s | Per CI-enabled repo: read the pipeline at HEAD, reconcile `cron_schedules` against it (upsert live exprs, delete stale), fire whatever is due. Sub-minute ticks so jitter can't skip a window. |
| CI/agent runner | `runCIRunner` → `ciRunner.run` (`ci_runner.go:211`) | `CIPollInterval` (default 5 s) | The work engine. See below. |

### The runner and the shared budget

`ciRunner.run` does three things at startup, before taking any work — nothing
can legitimately be in flight yet, so anything that looks live is crash debris:

1. `storage.ReconcileOrphanRuns` — finalize runs stranded `running` by a crash,
   instead of leaving the UI with a job stuck forever.
2. `storage.RevokeAgentRunTokens` — revoke ephemeral run tokens a crash left
   valid.
3. `sweepOrphanContainers` (docker isolation only) — reap leftover job
   containers.

Then it polls. Each tick drains, in order: CI runs, each kind in
`storage.AgentRunKinds` (`agent`, `spec-verify` — iterating the slice means a
new agent kind needs no edit here), expired parked agent sessions, queued
follow-up turns, and accepted handoffs.

**One budget, two claims** (`cmd/moongitd/budget.go`). CI and agent-family runs
draw from a single pool of `MaxConcurrency` slots (default `NumCPU`), with
`AgentReserved` slots (default 2) that only agents may take:

- `shared` channel, capacity `total` — caps everyone.
- `ciAllow` channel, capacity `total - reserved` — caps CI.
- A CI run holds one of each (`ciAllow` first, so a CI run never sits on a
  shared slot while blocked on its own cap); an agent run holds only a shared
  slot.

Invariants: total in flight ≤ `total`, CI ≤ `total - reserved`, and at least
`reserved` shared slots are reachable only by agents — so a CI backlog can never
lock out an agent spawn. **Every acquire is non-blocking.** A full budget means
"skip this tick, retry next poll"; blocking would let one kind starve the other
inside the drain loop.

### Shutdown drain

The ordering in `runServe` is deliberate and fragile enough to call out. On
SIGINT/SIGTERM *or* a listener error:

1. `httpSrv.Shutdown` with a 10 s timeout.
2. If SSH is enabled: `stop()` then `<-sshDone`. The explicit `stop()` matters —
   on the serve-error path the signal context was never cancelled.
3. `cancelCI()` then `<-ciDone`. `ciCtx` has its own cancel for the same reason.
4. Only then do the deferred `rdb.Close()` / `db.Close()` fire.

The point: an in-flight run must write its terminal status against a **still-open
DB**. Close the DB first and a clean restart strands the run `running` forever.
The CI drain is intentionally **unbounded** — a wedged step hangs here rather
than racing `db.Close()` under a timeout; systemd's `TimeoutStopSec` then
SIGKILLs us, and `ReconcileOrphanRuns` cleans up on the next boot. That is the
designed division of labor: the drain handles clean exits, reconcile handles
SIGKILL and power loss.

---

## The web SPA

Vite + React 19, built to `web/dist`, served by the same process from
`MOONGIT_WEB_DIR` via `webHandler` (`internal/server/web.go`): existing file →
serve it, anything else → `index.html` so the client router owns the route. When
`MOONGIT_WEB_DIR` is unset the handler is `nil` and the process runs API+git
only.

Routing lives in `web/src/App.tsx`: a `TokenGate` in front of everything (no
token → no app), then static top-level aggregate routes (`/issues`, `/pulls`,
`/pipelines`, `/agents`) declared **before** the `/:owner/:repo` dynamic routes
so they out-rank it, then the per-repo routes. These aggregates are backed by
the cross-repo endpoints `GET /api/issues`, `/api/pulls`, `/api/runs`.

**Conventions are owned by [`web/CLAUDE.md`](../web/CLAUDE.md)** — feature-first
layout under `src/features/`, the `@/` import alias, the `src/ui/` primitive
library and its `/dev/ui` gallery, semantic BEM styling, `clsx`, Biome, and
Playwright. Read that file before touching the frontend; it is not restated
here.

---

## Where to make a change

| I want to… | Touch |
|---|---|
| **Add an API endpoint** | Register the pattern in `apiHandler()` (`internal/server/server.go:143`) — or `publicAPIHandler()` if it must work without a token. Add the handler in the matching `internal/server/<domain>.go`. Reads take `s.rdb`, writes take `s.db`. Request/response structs go in `internal/api/types.go`. A repo-scoped route is gated automatically by `withRepoAccess` (`internal/server/access.go`) — don't re-check in the handler. Test next to the handler (`internal/server/<domain>_test.go`). |
| **Add a table or column** | Append a new string to `migrations` in `internal/storage/migrations.go` — never edit an existing one. Add the query functions in `internal/storage/<domain>.go` taking `*sql.DB` as the first arg so the caller picks the pool. Cover it in `internal/storage/migrations_test.go` plus a domain test. |
| **Change git behavior (clone/push/merge)** | Transport: `internal/server/git.go`. Route matching: `isGitRequest` in `internal/server/web.go`. Merge/ref movement: `internal/server/merge.go` (stay worktree-free). Repo creation + hook install: `internal/server/repos.go`, `internal/server/ci_hook.go`. SSH transport: `internal/server/ssh.go`. |
| **Add a CI feature** | Pipeline schema + parsing: `internal/ci/ci.go`, `internal/ci/translate.go`. Event log format: `internal/ci/events.go`. Execution/orchestration: `cmd/moongitd/ci_runner.go`. Enqueue-on-push: `internal/server/ci_hook.go`. Run/job rows: `internal/storage/ci.go`. HTTP surface: `internal/server/ci.go`. |
| **Change agent-run behavior** | Spawn: `handleSpawnAgent` in `internal/server/issues.go`. Turn loop + executor: `cmd/moongitd/agent_runner.go`, `agent_executor.go`, `agent_launch.go`, `agent_stream.go`. Credentials: `agent_creds.go`. Handoff/finish: `agent_handoff.go`. Turn rows: `internal/storage/agent_turns.go`. Cancel path: `AgentCanceler` in `internal/server/server.go`. |
| **Change scheduling / concurrency** | Slot policy: `cmd/moongitd/budget.go` (+ `budget_test.go`). Drain order and poll cadence: `ciRunner.run` in `cmd/moongitd/ci_runner.go`. Cron: `cmd/moongitd/cron_scheduler.go`. |
| **Add a UI page** | Add the component under `web/src/features/<feature>/`, register the route in `web/src/App.tsx` (static routes before `/:owner/:repo`), wire data through `web/src/api/queries.ts` / `mutations.ts` and types in `web/src/api/types.ts`. Feature CSS in `web/src/features/<feature>/<feature>.css`. New shared primitive → `web/src/ui/` **and** a `/dev/ui` gallery row. Follow `web/CLAUDE.md`. |
| **Add a UI primitive** | `web/src/ui/` + barrel export + a section in `DevGalleryPage.tsx`; base styles in `web/src/styles.css`. |
| **Add a config knob** | `internal/config/config.go` (`MOONGIT_*`, with a default), document it in the `printUsage` block in `cmd/moongitd/main.go` if it's operator-facing. |
| **Add a background loop** | A `run…` function in `cmd/moongitd/main.go` following the reaper shape (ticker, `ctx.Done()`, writer pool, no-op when disabled), started with the others in `runServe`. If it can be mid-work at shutdown, it needs a drain before `db.Close()`. |
| **Add an event type** | Emit through `internal/storage/events.go`; consumers are `GET /api/events` (`internal/server/events.go`) and the SPA's feed. |
| **Add an admin CLI subcommand** | The `switch` in `main()` (`cmd/moongitd/main.go:37`), a `run…` function, `printUsage`, and `openDB()` if it needs the database. |
