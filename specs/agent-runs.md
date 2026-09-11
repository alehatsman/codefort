---
id: agent-runs
status: draft
owners: [aleh]
covers:
  - "internal/server/agent.go"
  - "cmd/moongitd/agent_*.go"
  - "internal/storage/agent_turns.go"
  - "internal/server/settings.go"
  - "internal/storage/settings.go"
---
# Agent Runs

## Intent

An agent run turns an issue into a containerized Claude session that works the
issue against the repo and hands its result back as a branch and a comment. It
reuses the CI run spine end-to-end — claim, lease, concurrency, isolation, the
per-job event stream, reconcile, retention — so an agent run is "a CI run whose
job is an LLM," not a parallel subsystem. What's distinct is the conversation:
the run parks between turns so a human can steer it, and finishing it is an
explicit, server-side handoff (no push — moongitd owns the repo). Credentials
and repo access are minted per-run and torn down on finalize; nothing sensitive
lives in the image.

## Behavior

- WHEN a client spawns an agent for an issue, the server resolves the base ref to
  an immutable commit (default the repo HEAD) and enqueues an agent-kind run
  bound to that issue, stamping the trigger from the token; an unknown issue is a
  404.
- WHERE the spawn request sets a model or tool profile, an explicit valid
  value wins, else the operator default, else the built-in default; an
  unknown model or profile is rejected.
- WHILE an agent run executes, its progress streams over the same run/job event
  endpoints as a CI run (one job, "agent"), and it draws from the agent
  concurrency pool, separate from CI runs.
- WHEN the runner picks up an agent run, it checks out the base commit into a
  fresh workspace, opens a container, and runs the issue body as the first turn;
  on success the run *parks* in `awaiting_input` with its container left running,
  rather than terminating.
- IF turn 1 hits an infrastructure failure (checkout, container, exec), the run
  is finalized and its workspace/container/token cleaned up.
- WHEN a client posts a follow-up turn to a non-terminal agent run, it queues
  (behind any in-flight turn); the dispatcher resumes the same session, streams
  the response onto the run's event log, and re-parks in `awaiting_input`.
- WHERE the execution model is `claude-edit` — the only execution model — a
  turn drives a resumable headless Claude session that edits files.
- WHERE credentials are needed, they are injected per-run into the container and
  never baked into the image: a scoped LLM auth token (operator gateway bearer >
  operator OAuth > env OAuth > env API key), an ephemeral moongit token (revoked
  on finalize) plus the server URL for in-container git/mgit, and the dex
  endpoint/bearer/project when a hot index is configured.
- WHEN a client finishes a parked (`awaiting_input`) run, the run transitions to
  `finishing` and the runner materializes the workspace as a commit on
  `agent/issue-<n>` server-side (no push), posts a summary comment on the issue,
  tears down the container/workspace/token, and finalizes the run; a handoff
  failure finalizes the run errored with a failure comment and never leaves a
  half-written branch.
- IF a finish is requested on a run that is mid-turn or already terminal, it is
  refused as a conflict — only a parked run can be handed off.
- WHEN a client cancels a non-terminal run, the in-flight turn is interrupted, the
  workspace is discarded, and the run is canceled — valid from any non-terminal
  state (unlike finish, which keeps the work and only accepts a parked run); an
  already-terminal run is a conflict, and cancel requires the in-process runner.
- WHILE a turn runs, a per-turn wall-clock timeout bounds it (enforced as a
  process kill), and the Claude stream is translated line-by-line into transcript
  events tolerantly — an evolving inner schema or a non-JSON line never breaks the
  stream.

## Non-goals

- **The deterministic CI path.** Push/pipeline CI is the ci-pipelines spec; this
  spec covers only what differs for an LLM job (turns, parking, handoff, creds).
  The shared spine (claim/lease/concurrency/reconcile/retention/event stream) is
  specified there.
- **The agent container image.** What the image contains, how it's built, and
  how Claude plans internally live outside moongit. This spec stops at the env
  moongit injects and the contract that a turn edits `/work`.
- **The dex index itself.** Wiring the agent to a configured dex index is in
  scope; building/serving that index is the code-intel/dex domain.
- **The events feed.** Run lifecycle may surface on the fleet feed, but the SSE
  feed's delivery and store are the events-feed spec's concern.
- **Headless command execution.** Bash is not reliably unlockable in a
  headless subscription-auth Claude session, so `claude-edit` — now the only
  execution model — cannot run commands (tests, git, mgit) inside a turn; it
  only edits files. This was previously offset by the `mooncake-agent` model,
  which ran commands under its own control; with that model gone, this is a
  known capability gap, not a deliberate non-goal — closing it (e.g. a
  reliable headless Bash unlock, or a different execution path) is future
  work, tracked outside this spec.

## Checklist

- [x] Spawn an agent run from an issue; immutable base commit; token-stamped trigger
- [x] Per-run model / tool-profile resolution with validation
- [x] Turn 1 in a fresh checked-out workspace + container; park in awaiting_input
- [x] Follow-up turns queue, resume the session, stream, re-park
- [x] claude-edit as the single execution model over a shared spine
- [x] Per-run scoped credentials (LLM auth, ephemeral moongit token, dex) torn down on finalize
- [x] Finish = server-side handoff to agent/issue-<n> + summary comment; parked-only
- [x] Cancel/force-stop from any non-terminal state; discards the workspace
- [x] Per-turn timeout; schema-tolerant transcript translation
- [ ] Defined handoff branch naming / CAS policy — no silent force-overwrite (#197)
- [ ] Verified against the code by the verify workflow (flip to `living`)
