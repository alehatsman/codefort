---
id: ci-pipelines
status: draft
owners: [aleh]
covers:
  - "internal/server/ci.go"
  - "internal/server/ci_hook.go"
  - "cmd/codefortd/ci_runner.go"
  - "cmd/codefortd/cron_scheduler.go"
  - "internal/ci/**"
  - "internal/storage/ci.go"
---
# CI Pipelines

## Intent

CI turns a push into feedback. When code lands in a codefort repo, the daemon
enqueues a run, executes the repo's pipeline against that exact commit in an
isolated workspace, and streams the result back live. The whole thing runs
in-process in the single `codefortd` binary — no external runner fleet — so a
self-hosted box gets push-triggered CI with nothing else to operate. Runs are
durable across restarts and bounded in resource use (concurrency caps, run
timeout, retention) so an unattended fleet doesn't accumulate stuck runs or
unbounded history. Agent runs reuse this same claim/lease/execute spine.

## Behavior

- WHEN a client pushes, the repo's post-receive hook notifies the daemon's
  loopback CI endpoint once per pushed ref, and the daemon emits a push event to
  the fleet feed.
- WHERE a pushed ref is a branch delete (all-zero new SHA), no run is enqueued.
- WHILE CI is disabled for a repo, a push is accepted and surfaced on the feed
  but enqueues no run, so non-CI repos accumulate no canceled runs.
- WHEN a run is enqueued, the commit's subject and author are frozen onto the run
  at enqueue time, so the run records what it was for, not just a SHA.
- WHEN a client triggers a run for an arbitrary ref without pushing, the daemon
  resolves the ref to a commit against the bare repo and enqueues a run with
  event `manual`; rerunning an existing run enqueues a fresh run number for the
  same commit, never mutating the original (history is append-only).
- WHERE the loopback CI endpoint is hit, the request must originate from
  loopback and carry the per-process CI secret; it lives off the Bearer `/api`
  surface, and the secret is injected into the push hook's environment, never
  written to disk.
- WHILE the daemon runs, a scheduler ticks every 30 seconds — deliberately
  sub-minute, since a minute-aligned tick that drifts by a second would step over
  a one-minute cron window and silently skip a fire.
- WHEN the scheduler ticks, it reconciles each CI-enabled repo's stored schedule
  entries against the schedule declared by the pipeline at HEAD: entries the
  pipeline no longer declares are dropped, new ones are added, and an entry that
  survives keeps its last-fired time — so editing an unrelated schedule never
  resets the others.
- WHEN a schedule entry's next occurrence has come due, the daemon enqueues a run
  for the repo's current HEAD commit with event `schedule`, recording which cron
  expression fired it, and stamps the fire time so the same window cannot fire
  twice. An entry that has never fired is measured from one tick ago, so adding a
  schedule does not immediately backfire every missed occurrence.
- WHERE a repo has CI disabled, has no commits, or declares no pipeline at HEAD,
  nothing is scheduled — and a repo whose pipeline stopped declaring schedules has
  its stored entries cleared. IF the pipeline at HEAD is unparseable, or a cron
  expression is invalid, the scheduler leaves the stored entries alone and fires
  nothing rather than guessing.
- WHEN the runner picks up a queued run, it executes only if the repo is
  CI-enabled and a pipeline file exists at that commit; otherwise the run is
  marked canceled (not failed) — a commit carrying no pipeline is a no-op.
- WHEN a run executes, the runner materializes the repo tree at the run's commit
  into a fresh workspace, runs the pipeline's jobs there, and always cleans the
  workspace up afterward.
- WHILE a run's jobs form a dependency DAG, jobs whose dependencies have all
  succeeded run concurrently up to the job-concurrency cap; a job whose
  dependency failed or was skipped is itself skipped, cascading.
- WHERE isolation is `docker` (the default), each job runs in a throwaway
  container; isolation `none` runs steps on the host (for tests/constrained
  hosts).
- WHEN every job succeeds the run is `success`; if any job did not succeed it is
  `failed`; a run that exceeds the run timeout is `error`; a run cut short by
  daemon shutdown is `interrupted` (operator-induced, not a red failure).
- WHILE CI runs and agent runs execute, each draws from its own concurrency pool,
  so a burst of one kind cannot starve the other.
- WHEN the daemon starts, runs left `running` by a previous crash or restart are
  reconciled to a terminal status and leftover job containers are reaped, so no
  run is stuck forever.
- WHILE the daemon shuts down, it stops claiming new runs and drains in-flight
  runs before exiting.
- WHILE run retention is configured to keep N per repo, terminal runs beyond the
  N most recent are pruned along with their on-disk event logs; a non-positive N
  disables pruning.
- WHEN a client lists or fetches runs, it can filter by kind, state, query, and
  limit, get a run with its jobs, and subscribe to a job's event log over SSE —
  replayed from the start, live-tailed while in flight, resumable via
  Last-Event-ID, and closed once the run is terminal.

## Non-goals

- **Agent runs.** Issue-spawned containerized agent sessions reuse this run
  spine (claim, lease, concurrency, reconcile, retention, event stream) but are
  specified separately; this spec is the deterministic-pipeline path.
- **The pipeline file format.** The `codefort.yml` schema and how steps map onto
  mooncake are a configuration/mooncake concern, not specified here; this spec
  says the file gates and defines the job DAG, not its grammar.
- **The events feed.** Runs emit push/run events, but the SSE fleet feed's
  delivery and backing store are the events-feed spec's domain. The per-job
  event-log stream (replay/tail/resume) *is* in scope here.
- **Container image provisioning.** What the CI/agent image contains and how it's
  built lives outside codefort (mooncake task); this spec only states that docker
  isolation runs jobs in a container.
- **An external runner / agentd.** CI stays in-process by design; a separate
  runner daemon is explicitly deferred and out of scope.

## Checklist

- [x] Push → post-receive hook → loopback enqueue (secret-gated, loopback-only)
- [x] Branch deletes and CI-disabled repos enqueue nothing
- [x] Manual trigger by ref; append-only rerun with a new run number
- [x] Gate on CI-enabled + pipeline present; missing pipeline → canceled
- [x] Fresh per-run workspace at the commit, always cleaned up
- [x] Dependency-wave job scheduling with skip cascade, bounded by job cap
- [x] docker (default) / none isolation
- [x] Terminal status mapping (success/failed/error/interrupted/canceled)
- [x] Independent run vs agent concurrency pools
- [x] Startup orphan reconcile + container reap; graceful shutdown drain
- [x] Per-repo run retention pruning rows + on-disk logs
- [x] Run list/get + resumable per-job SSE event stream
- [x] 30-second scheduler tick (sub-minute so a cron window is never skipped)
- [x] Per-repo schedule reconcile against the pipeline at HEAD; last-fired preserved
- [x] Due entries enqueue a `schedule` run at HEAD, recording the firing expression
- [x] CI-disabled / no-commit / no-pipeline / unparseable repos schedule nothing
- [ ] Verified against the code by the verify workflow (flip to `living`)
