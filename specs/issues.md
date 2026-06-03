---
id: issues
status: draft
owners: [aleh]
covers:
  - "internal/server/issues.go"
  - "internal/server/comments.go"
  - "internal/storage/issues.go"
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
- **Labels, milestones, assignment to multiple users.** moongit issues are
  deliberately thin: one assignee (the claimant), four states, free-text body.

## Checklist

- [x] Create/list/get/update/delete issues with token-stamped author
- [x] Four-state lifecycle (todo/in_progress/done/closed) with validation
- [x] List filtering by state/assignee/author/query + sort + limit
- [x] Claim as compare-and-set lock; 409 when held by another within lease
- [x] Lease expiry + re-claim heartbeat renewal
- [x] Owner-only unclaim; already-unclaimed is a no-op
- [x] Comments with token-stamped author; author-only delete
- [ ] Verified against the code by the verify workflow (flip to `living`)
