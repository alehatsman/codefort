---
id: events-feed
status: draft
owners: [aleh]
covers:
  - "internal/server/events.go"
  - "internal/storage/events.go"
  - "cmd/moongit/events.go"
---
# Events Feed

## Intent

The events feed is how a fleet of agents stays aware of each other without
polling every endpoint. Every meaningful action on the data plane — a push, an
issue claim, a CI run starting or finishing — appends a row to a durable events
table, and `GET /api/events` streams that table as Server-Sent Events: replay
what you missed, then live-tail what's new. It is deliberately **pull-based**:
moongit makes no outbound calls (no webhooks, #73), so subscribers reach in over
the same authenticated API and there's nothing to configure or to fail
delivering. The feed is a notification side-channel — best-effort and never on
the critical path of the action it reports.

## Behavior

- WHEN a data-plane action occurs (push, issue create/update/state/claim/comment,
  CI run queued/finished, repo deleted, agent lifecycle), the server appends one
  event carrying a monotonic sequence number, a type, the actor, the repo, and a
  JSON payload.
- WHILE an event is being emitted, a failure to record it is logged but never
  propagated — the triggering mutation always succeeds regardless, because the
  feed must not be on the action's critical path.
- WHEN a client opens `GET /api/events`, the server replays the events table from
  the client's resume point and then live-tails new rows, holding the connection
  open until the client disconnects (the feed never self-closes).
- WHERE a client reconnects with a Last-Event-ID (or `?last_event_id=`), only
  events with a higher sequence number are sent, so a dropped connection resumes
  without gaps or duplicates.
- WHERE `?once=true` is set, the server replays the available backlog and then
  closes instead of tailing — the snapshot mode the CLI's `--once` uses.
- WHERE `?repo=owner/name` is set, the stream is scoped to that repo (an unknown
  repo is a 404, not a silently empty stream); `?types=a,b` filters to the named
  event types, and an empty filter passes all types.
- WHILE a large backlog is replayed, events are read in bounded batches and
  drained without sleeping until caught up, then the stream tails at a sub-second
  cadence with a periodic heartbeat so idle connections aren't dropped and a gone
  client is noticed promptly.
- IF a stored event's payload is corrupt, the event still streams with its
  sequence and type intact and a null data field, rather than being dropped.
- WHILE the daemon runs, a retention sweep bounds the table to the newest
  configured number of events, and deleting a repo cascades away its events.
- WHEN a client uses the `mgit events` command, it subscribes to this feed and
  prints events as they arrive, with a snapshot (`--once`) mode over the same
  endpoint.

## Non-goals

- **What each subsystem emits.** This spec owns the feed's transport, ordering,
  replay, and retention; the specific events and when they fire are each
  subsystem's spec (issues, ci-pipelines, agent-runs, pull-requests, git-hosting).
- **The per-job transcript stream.** The live CI/agent job event log
  (`runs/{n}/jobs/{job}/events`, replay+tail+resume, self-closing on terminal) is
  a different stream specified by ci-pipelines; this feed is the cross-cutting
  fleet feed.
- **Webhooks / outbound delivery.** Pushing events to external URLs was
  explicitly rejected in favor of pull-based SSE (#73). No retries, no signing,
  no delivery receipts.
- **Exactly-once / guaranteed delivery.** The feed is best-effort: an emit that
  fails to record is dropped (the action still succeeds), and retention can age
  out old events. Resume is gap-free only within the retained window.
- **Authorization granularity.** Any valid token may subscribe to the whole feed;
  per-event access control is out of scope under the local-trust posture
  (identity-and-tokens).

## Checklist

- [x] Durable, append-only events table with a monotonic sequence
- [x] Best-effort emit that never fails the triggering mutation
- [x] `GET /api/events` SSE: replay then live-tail, long-lived
- [x] Resume via Last-Event-ID / `?last_event_id=` (higher-seq only)
- [x] `?once=` snapshot, `?repo=` scope (404 unknown), `?types=` filter
- [x] Bounded batch drain + sub-second tail + heartbeat
- [x] Corrupt-payload event still streams (seq/type intact, null data)
- [x] Retention sweep to newest-N; repo delete cascades events
- [x] `mgit events` client (stream + `--once`)
- [ ] Verified against the code by the verify workflow (flip to `living`)
