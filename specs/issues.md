---
id: issues
status: draft
owners: [aleh]
covers:
  - "internal/server/issues.go"
  - "internal/server/comments.go"
  - "internal/storage/issues.go"
  - "internal/storage/dependencies.go"
  - "cmd/moongit/main.go"
---
# Issues & Claim-First Coordination

## Intent

Issues are moongit's coordination layer for a fleet of agents (and humans)
sharing one set of repos. Beyond tracking work, an issue is a **lock**: claiming
it announces "I own this," and a claim held under another identity means the
work is taken. This is how concurrent agents avoid stepping on each other
without a central scheduler — the claim is a cooperative, leased advisory lock,
not hard access control. The data plane stays intentionally open (any valid
token may write); the claim is the social contract that keeps it orderly.

## Behavior

- WHEN a client creates an issue with a title, the server stores it under a
  per-repo issue number and stamps the author from the authenticated token,
  ignoring any client-supplied author.
- IF a create or update request has a blank title, it is rejected — title is
  required and cannot be blanked.
- WHERE an issue state is set, it must be one of `todo`, `in_progress`, `done`,
  or `closed`; any other value is rejected.
- WHEN a client lists issues, the server filters by state (comma-separated),
  assignee, author, and a free-text query, and honors sort and limit.
- WHEN an issue's state changes, the server emits a state-change event to the
  fleet feed; a title/body-only edit emits a plainer update event.
- WHEN a client claims an unclaimed issue (or one whose claim has expired, or one
  it already holds), the claim succeeds, stamps the assignee from the token, and
  records the claim time; an optional state (e.g. `in_progress`) is applied in
  the same step.
- IF a client claims an issue currently held by a different identity within the
  lease window, the claim is refused as a conflict.
- WHILE a claim lease is configured and positive, a claim stays exclusive only
  for the lease duration; after it elapses without renewal, another identity may
  claim. A lease of zero means claims never expire.
- WHEN the current owner re-claims an issue it already holds, the claim time is
  refreshed — re-claiming is the heartbeat that renews the lease.
- WHEN a client unclaims an issue it owns, the assignee and claim are cleared;
  unclaiming an already-unclaimed issue is a no-op success.
- IF a client unclaims an issue held by a different identity, it is refused as
  forbidden — only the owner releases its own claim.
- WHEN a client comments on an issue with a non-empty body, the comment is stored
  with the author stamped from the token and an event is emitted to the feed;
  only the comment's author may delete it.
- WHILE a request carries a valid token, any identity may create, update, delete,
  or claim issues — the claim, not per-issue ownership of the data, is what
  serializes work.

### Labels

Labels are the one piece of free-form taxonomy on an issue — cheap to add, with
no registry to keep in sync.

- WHEN a client creates an issue with labels, they are stored as given; an issue
  with no labels reports an empty set, never a null one, so a client never has to
  special-case "never labeled."
- WHERE an update carries a label set, that set **replaces** the issue's labels
  wholesale; omitting labels leaves them untouched, and an explicitly empty set
  clears them. The difference between "not mentioned" and "cleared" is deliberate
  — a partial edit of title or state can never silently drop an issue's labels.
- WHEN a client lists issues filtered by a label, only issues carrying that exact
  label (case-sensitive, one label per request) are returned; the filter composes
  with state/assignee/author/query and applies to both a repo's backlog and the
  cross-repo aggregate feed.
- WHILE labels stay free-form strings, there is no per-repo label registry,
  colour, or description to administer — a label exists because an issue carries
  it. That is why the labels ride on the issue itself as a JSON array rather than
  a join table: 0-N tags without a second entity and its own CRUD surface.

### Typed edges between issues

Issues form a graph through two typed, directed edges, so a fleet can answer
"what's next" from structure instead of hand-maintained prose.

- WHERE an issue carries a **parent**, it is a child of that epic; an issue has
  at most one parent (a per-repo issue number), and setting it to a missing or
  self issue is rejected. Setting the parent to zero clears it.
- WHEN a client adds a **depends-on** edge from issue A to issue B, A is recorded
  as blocked until B is done; both must exist in the repo, a self-edge is
  rejected, and an edge that would close a cycle is refused (the dependency graph
  is kept acyclic so the derived "ready" view is well-defined).
- WHEN a client removes a depends-on edge, the edge is dropped; removing an edge
  that does not exist is an idempotent success, as is adding one that already
  exists.
- WHEN a single issue is fetched, the response carries its edge sets — children,
  the issues it is blocked by (depends-on targets), and the issues it blocks —
  each as a lightweight number/title/state reference.
- WHEN an issue is deleted, its depends-on edges are cleared in both directions
  so no dangling edge survives.

### Computed backlog views

The dependency graph powers two derived, live-computed list filters — no manual
`ready`/`blocked` label to keep in sync.

- WHEN a client lists a repo's issues with the **ready** filter, the result is
  the actionable pick list: issues that are `todo`, unclaimed, not an epic (have
  no children), and whose every depends-on target is in a done/closed state.
- WHEN a client lists with the **blocked** filter, the result is the inverse for
  visibility: `todo` leaves (non-epics) with at least one depends-on target not
  yet done/closed.
- IF a request asks for both ready and blocked at once, it is rejected — the two
  views are mutually exclusive.
- WHERE ready/blocked are requested, they apply to a single repo's backlog; the
  cross-repo aggregate feed ignores them.

### Native epic rollup

Epic↔child structure is first-class in the views, so a drifting prose checklist
is never needed.

- WHEN a single issue with children is fetched, the response carries a live
  child-completion rollup (how many children are done/closed out of the total),
  computed from parent edges + child states.
- WHEN a client lists with the **epics** filter, the result is only umbrella
  issues (those with at least one child), each carrying its rollup; combining
  epics with ready or blocked is rejected (those views exclude umbrellas).
- WHERE a rollup is reported, it reflects the current child states with no manual
  edit to the epic — marking a child done updates the parent's rollup directly.

## Non-goals

- **Agent runs spawned from an issue.** Turning an issue into a containerized
  agent run (`POST .../issues/{n}/agent`) is its own subsystem with its own spec;
  here an issue is just the unit of work and its lock.
- **Pull requests & review.** Linking issues to branches/PRs and the merge flow
  belong to the pull-requests spec.
- **The events feed itself.** Issue actions emit events, but the SSE feed's
  delivery, schema, and backing store are specified by the events-feed spec; this
  spec only states *that* the relevant actions emit.
- **Hard authorization & roles.** No per-repo permissions, no admin override on
  claims, no locking down delete. The open data plane is deliberate; the claim is
  advisory. Identity hardening lives in the auth/tokens spec.
- **Milestones, multi-assignee, a managed label taxonomy.** moongit issues stay
  deliberately thin: one assignee (the claimant), four states, free-text body.
  Labels exist (above) but only as free-form tags on the issue — no label
  registry, colours, descriptions, or rename/merge operations.

## Checklist

- [x] Create/list/get/update/delete issues with token-stamped author
- [x] Four-state lifecycle (todo/in_progress/done/closed) with validation
- [x] List filtering by state/assignee/author/query + sort + limit
- [x] Claim as compare-and-set lock; 409 when held by another within lease
- [x] Lease expiry + re-claim heartbeat renewal
- [x] Owner-only unclaim; already-unclaimed is a no-op
- [x] Comments with token-stamped author; author-only delete
- [x] Parent edge (epic membership): at most one, validated, clearable
- [x] depends-on edges: add/remove, self + cycle rejection, edges in single-issue GET, cleared on delete
- [x] Computed `ready` / `blocked` backlog views over the dependency graph; mutually exclusive
- [x] Native epic rollup: child-completion progress on single GET + `epics` list view
- [x] Free-form labels on create; replace-whole-set on update (nil = no change, empty = clear)
- [x] Labels stored on the issue as a JSON array — no label table, no registry
- [x] Exact-match label filter on both the per-repo and cross-repo issue lists
- [ ] Verified against the code by the verify workflow (flip to `living`)
