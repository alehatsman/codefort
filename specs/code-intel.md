---
id: code-intel
status: draft
owners: [aleh]
covers:
  - "internal/server/intel.go"
  - "internal/dex/**"
---
# Code Intelligence (dex / Explore)

## Intent

Code intelligence is moongit's read-only view of what the code *is* — the dual
of a spec, which says what it *should* do. It is powered by an external dex
embedding server: moongit proxies dex's index status, composed summaries,
package-import graph, and semantic/symbol search through its own authenticated
API, scoped to the dex project that matches a repo. The integration is strictly
**optional and best-effort**: dex is a separate service that may be unset, down,
or simply hasn't indexed a given repo, and none of those states is an error to
moongit — they are distinct, expected conditions the UI renders rather than
failures that break a page.

## Behavior

- WHILE dex is not configured, the integration is disabled: the status endpoint
  reports `enabled: false` with a 200, and the data endpoints (overview, package
  graph, file summary, summaries, search) return a clean service-unavailable.
- WHEN a client requests intel status for a repo, the server reports whether dex
  is configured, whether it's reachable, and whether a dex project matches the
  repo (matched by name) with that project's index stats — never failing just
  because dex is down or the repo is unindexed; only a missing repo (404) or a
  dex transport error (502) is an error.
- WHEN a client requests any data endpoint, the server resolves the repo to its
  dex project first; an unindexed repo is a 404 and an unreachable dex is a 502,
  so the two are always distinguishable.
- WHEN a client requests the overview, the server returns the repo- and
  package-level summaries dex composed at index time.
- WHEN a client requests the package graph, the server returns dex's internal
  package-import DAG; a non-Go or un-graphed project comes back as a 200 with a
  "no-graph" status and no nodes, so the caller degrades to a flat listing
  instead of erroring.
- WHEN a client requests a file summary for a path, the server returns dex's
  summary for that file, or a 200 with an empty summary when dex has none — most
  files are not summarized, and that is not an error.
- WHEN a client requests all summaries, the server returns one path→prose map for
  the whole repo in a single dex call (the repo-root summary folded onto the empty
  key), so the UI can resolve breadcrumbs and tree rows without per-path recall.
- WHEN a client searches, the server proxies the query to dex scoped to the repo's
  project, supporting semantic (default), symbol, ask, callers, and callees
  modes, and caps the number of hits.
- WHERE any dex call fails at the transport level, the server surfaces it as a
  bad-gateway with the underlying error, rather than masking it as an empty
  result.

## Non-goals

- **dex itself.** Indexing, embeddings, summary composition, the import-graph
  computation, and the reindex/watch lifecycle all live in the dex project, not
  moongit. This spec covers only moongit's proxy and how it degrades.
- **The Explore rendering.** Laying out the package graph as a map, filtering
  isolated nodes, and deriving the module prefix are web-side concerns; this spec
  stops at the data the server returns.
- **The per-file summary card / blob view UI.** Where and how summaries render is
  a web concern; here only the endpoint contract.
- **Agent dex wiring.** Injecting a dex endpoint/bearer into an agent container is
  the agent-runs spec; this spec is the human/UI read surface.
- **Authorization.** Any valid token may query intel for any repo, per the
  local-trust posture (identity-and-tokens).

## Checklist

- [x] Optional integration: disabled → enabled:false (200) / 503 on data endpoints
- [x] Status reports configured/reachable/project-match without erroring on dex-down
- [x] Repo→dex-project resolution; unindexed → 404, unreachable → 502 (distinct)
- [x] Overview (repo + package summaries composed at index time)
- [x] Package graph proxy; non-Go/un-graphed → 200 "no-graph", no nodes
- [x] File summary; missing → 200 empty (not an error)
- [x] All-summaries path→prose map in one call (repo root folded onto "")
- [x] Search proxy: semantic/symbol/ask/callers/callees, capped hits
- [x] Transport failures surfaced as 502 with the underlying error
- [ ] Verified against the code by the verify workflow (flip to `living`)
