---
id: constitution
status: living
owners: [aleh]
---
# Constitution

## Intent

This is the repo-wide contract every other spec inherits. codefort is a
self-hosted git host, issue tracker, and agent-coordination backend for a small,
trusted fleet — not a public forge. These principles are the *why* behind the
per-subsystem specs; where they bear on a feature, that spec applies them rather
than restating them. They change rarely and deliberately; a sibling spec that
needs to contradict one should say so explicitly and link here. Keeping them in
one place is what lets the rest of the specs stay short.

## Behavior

- WHILE codefort runs, it is a single binary: one `codefortd` process serves the
  `/api` surface, git smart-HTTP, the web SPA, and the in-process CI/agent runner
  on one port — a self-hostable box gets the whole system with nothing else to
  operate.
- WHERE access is gated, the posture is local-trust: a valid token is the bar
  (authentication for attribution and a network gate), and the data plane is
  open by default — coordination comes from the claim lock and author-only
  deletes, not from roles or per-resource authorization.
- WHERE a team wants less than that openness, a repo may be marked private and
  granted to named accounts with a coarse owner/write/read role, enforced
  uniformly on both the API and the git transport (access-control). This is a
  convenience layered on local-trust, not a replacement for it: it is opt-in,
  every repo is public by default, and the vocabulary stops at three roles —
  the *grid* of per-user, per-resource rules stays out.
- WHEN any write occurs, it is attributed to the acting token's identity; agents
  act under distinct identities and coordinate claim-first, so concurrent actors
  serialize without a central scheduler.
- WHERE state lives, git is the substrate: bare repositories on disk are the
  source of truth, server-side git operations are worktree-free (safe against the
  repo being served), the canonical branch advances through pull-request merges,
  and history is append-only (reruns and superseded specs are kept, not rewritten).
- WHERE notifications flow, they are pull-based: subscribers reach in over the
  authenticated SSE feed and codefort makes no outbound calls (no webhooks), so
  there is nothing to configure or fail delivering.
- WHERE a capability beyond the core is added (SSH transport, CI, agent runs),
  it is opt-in and additive: the default deployment stays a single open HTTP
  port, and turning the capability off never changes the core's behavior.
- WHERE specs exist, they are the dual of the code: a spec says what the system
  *should* do, the code is what it *is*, and the gap between them is drift —
  a signal surfaced to humans, not a merge blocker, at least until the workflow
  earns trust.
- WHILE the system evolves, simplicity wins: boring, explicit mechanisms (a
  single-writer SQLite pool with a separate read pool, plain git plumbing) are
  preferred over abstraction and magic, and scope is added only when a real need
  demands it.
- WHILE codefort is developed, it is dogfooded: this repo's own work is tracked as
  codefort issues, claimed before coding, merged through codefort pull requests, and
  specified by these specs — the backend coordinates its own development.

## Non-goals

- **A public, multi-tenant forge.** No org hierarchy, forks, or abuse controls.
  codefort hosts a known fleet's repos on a trusted box. Named accounts exist, but
  as identities and coarse grantees — not as tenants.
- **A fine-grained authorization system.** Coarse repo access (owner/write/read)
  is the ceiling; per-resource ACLs, custom roles, scopes, and expiring/scoped
  tokens are deliberately absent (see access-control and identity-and-tokens).
  The distinction that matters: a handful of optional, coarse grants is a
  convenience, while a permission matrix would make authorization load-bearing,
  which local-trust exists to avoid.
- **Horizontal scale / high availability.** The single-binary, single-writer
  design targets one box; clustering, replication, and failover are out of scope
  (any such effort is a separate, explicitly-flagged design).
- **A webhook / outbound-integration hub.** Pull-based SSE is the chosen feed
  shape; pushing to external systems is not a goal.
- **Semantic code intelligence.** Embedding indexes, symbol graphs, and
  meaning-based search over the code or the specs are not codefort's business.
  The dex integration that once provided them was an experiment and has been
  removed wholesale; anything in this shape is a separate tool codefort does not
  depend on.
- **Restating subsystem detail.** This spec holds principles only; the concrete
  behavior of each capability lives in its own spec.

## Checklist

- [x] Single binary serves /api + git smart-HTTP + SPA + in-process runner, one port
- [x] Local-trust: valid token = the bar; open data plane; claims are the social lock
- [x] Optional coarse repo access (private + owner/write/read), enforced on API and git
- [x] Every write attributed to the acting identity; claim-first coordination
- [x] Git as source of truth; worktree-free server ops; PR-advanced main; append-only
- [x] Pull-based SSE feed; no outbound webhooks
- [x] Beyond-core capabilities (SSH/CI/agents) are opt-in; single open HTTP port default
- [x] Specs are the dual of code; drift is a non-blocking signal
- [x] Boring/explicit tech; simplicity over abstraction
- [x] codefort dogfoods its own coordination (issues/claims/PRs/specs)
