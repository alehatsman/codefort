# moongit — Vision

A self-hosted git host, issue tracker, and CI runner that fits in your head and
runs on your own machine. One binary, one port, one file of state.

moongit exists because the tools we use to coordinate software have drifted far
from the work itself. A git host should not need a cluster, a managed database,
an object store, a message queue, and a build farm to let a few people share
code and track what needs doing. moongit is the bet that it never did.

## The three commitments

### 1. Absolute minimalism

Every part you can remove is a part that can't break, can't be misconfigured,
and doesn't need to be understood by the next person who reads the source.

- **One process.** `moongitd` serves the API, git smart-HTTP, and the web SPA
  on a single port. There is no sidecar, no reverse proxy requirement, no
  separate worker pool to keep alive. (The git SSH transport is the one
  optional second listener — off unless `MOONGIT_SSH_ADDR` is set, and still
  the same process. The default stays one port.)
- **One file of state.** SQLite via `modernc.org/sqlite` — pure Go, no CGO, no
  external database to provision, back up, or babysit. Copy the file and you've
  copied the server.
- **A dependency list you can read in one screen.** The whole point is that the
  `go.mod` stays short. New direct dependencies are a cost paid in trust and
  longevity, not a convenience — they earn their place or they don't come in.
- **No required ecosystem.** No Docker to run it, no Kubernetes to scale it, no
  cloud account to host it. A binary and a data directory.
- **The client is small too.** `mgit` is a thin, scriptable CLI. Identity is a
  token, claims are rows, coordination is plain HTTP. Nothing to learn that
  isn't already a git or REST concept.

Minimalism is not a phase to be grown out of. It is the feature.

### 2. High performance

Small systems are fast systems, almost for free — but we hold the line on
purpose.

- **Startup is instant.** No warm-up, no JIT, no connection pools to fill before
  the first request is served. Cold start to serving is the cost of opening a
  file.
- **Native speed, native memory.** Compiled Go, a single embedded database, and
  data structures sized for a team — not a hyperscaler. Latency is dominated by
  disk and the network, not by our own layers.
- **The SPA ships as static files** served by the same process. No build step at
  request time, no server-side rendering tier, no hydration tax.
- **Operations are O(what you'd expect).** Listing issues reads issues. Cloning
  a repo streams a pack. There is no hidden fan-out, no N+1 across services,
  because there are no other services.

Performance here means the tool disappears: you stop noticing it because it is
never the slow part.

### 3. Local first

Your code, your issues, your history — on hardware you control, working whether
or not the internet does.

- **Offline is the default, not a degraded mode.** Everything that matters lives
  in the local data directory. The network is for sharing, not for existing.
- **You own the data.** It's a directory and a SQLite file. Inspect it, back it
  up with `cp`, move it to a new box, or read it with any SQLite tool. No export
  ritual, no vendor lock, no API you have to beg for your own data.
- **Trust is local.** A token names an identity; the data plane is intentionally
  open within a trusted network. moongit assumes a small group that already
  trusts each other, not an adversarial public internet — and is far simpler for
  it. This still leaves room for *lightweight* refinements on top of that base —
  named user accounts, a handful of branch-protection rules — as conveniences
  for a trusting team (avoiding mistakes, attributing work), not as a security
  perimeter policing an adversary. The line is coarse-and-optional, not
  fine-grained-and-load-bearing.
- **Self-hosting is a first-class path, not a fallback.** The same binary you'd
  run "in production" is the one you run on your laptop. There is no hosted
  edition that the open one quietly trails behind.

## What this rules out

A vision is also a set of nos. To stay true to the three commitments, moongit
will not:

- grow a microservice architecture, a required external database, or a
  background-worker tier;
- adopt heavy frameworks on either the server or the web client when the
  standard library and a small SPA suffice;
- add features that only make sense at a scale moongit is not built for (orgs of
  thousands, public multi-tenant hosting, fine-grained RBAC matrices — the *grid*
  of per-user, per-resource permission rules, not coarse accounts or a few branch
  rules, which the trust model above permits);
- trade startup time, binary size, or operational simplicity for breadth of
  features.

When a proposed change pulls against minimalism, performance, or local-first
ownership, the default answer is no — and the burden is on the change to prove
it belongs.

## The test for any change

Before adding anything, ask:

1. Does it keep the system to one process, one file of state, and a short
   dependency list?
2. Is it fast enough to go unnoticed, with no warm-up and no hidden fan-out?
3. Does it work fully offline, on hardware the user owns, with data they can
   copy?

If the answer to any of these is no, the feature is wrong for moongit — or
moongit is wrong, and we should say so plainly rather than compromise the three
commitments.
