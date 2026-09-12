---
id: git-hosting
status: draft
owners: [aleh]
covers:
  - "internal/server/git.go"
  - "internal/server/repos.go"
  - "internal/server/tree.go"
  - "internal/server/commits.go"
  - "internal/server/branches.go"
  - "internal/server/server.go"
  - "internal/storage/repos.go"
---
# Git Hosting

## Intent

codefort is a self-hosted git host: a single binary that stores bare git
repositories on disk and serves them over git's smart-HTTP protocol, so any
stock `git` client can clone, fetch, and push without a special client. On top
of that it exposes read-only views of repository contents — branches, file
trees, blobs, commit history, diffs — so a repo can be browsed without a
checkout. This is the floor a local-first GitHub stands on; issues, pull
requests, CI, and agents all assume a repo already lives and moves here.

## Behavior

- WHEN a client POSTs to `/api/repos` with an owner and name, the server
  provisions a bare repository on disk and registers it, returning the repo
  summary.
- WHERE an owner or repo name contains anything outside letters, digits, `.`,
  `_`, `-`, or is `.`/`..`/over 100 chars, repo creation is rejected as invalid.
- IF a repo with the same owner and name is already registered, creation fails
  with a conflict rather than silently reusing it.
- WHEN a repo is created, its default branch HEAD is pinned to `main`, so the
  first push of a `main` branch is browsable rather than landing under an unborn
  `master`.
- WHEN a stock `git` client requests `info/refs` for `git-upload-pack` or
  `git-receive-pack`, the server advertises the repo's refs in pkt-line framing.
- WHEN a client fetches or clones (`git-upload-pack`) or pushes
  (`git-receive-pack`), the server streams the request to the real `git` binary
  and streams its response back, so standard clients work unmodified.
- WHERE a request path uses `{owner}/{repo}` or `{owner}/{repo}.git`, both
  resolve to the same on-disk repository; any path that escapes the repos
  directory is rejected.
- IF a git request targets a repository that does not exist on disk, the server
  responds 404 rather than creating it implicitly.
- WHEN a client GETs the read API (`refs`, `tree`, `blob`, `raw`, `commits`,
  `commit/{sha}`, `tree-commits`, `compare`), the server returns that repo
  content for the requested ref, defaulting to the repository's HEAD branch.
- WHILE a repository has in-flight CI/agent runs, deleting it is refused with a
  conflict; otherwise deletion removes its registration, its on-disk bare repo,
  and its run logs.
- WHILE `CODEFORT_BASIC_USER` is set, git smart-HTTP and the web UI require HTTP
  Basic credentials; unset, they stay open. The `/api` surface always uses its
  own Bearer-token auth regardless.
- WHILE the daemon runs, one process serves the `/api` surface, git smart-HTTP,
  and the SPA on a single port.

## Non-goals

- **SSH transport.** Git over SSH is a separate, additive transport with its own
  spec; this spec is the HTTP path only.
- **Authentication & identity.** How tokens are minted, how Bearer/Basic
  identity is resolved, and the open-data-plane posture are owned by the
  auth/tokens spec. This spec only states *where* auth applies.
- **Push side effects.** The post-receive hook firing CI on push is real but
  belongs to the CI pipeline spec; here it is mentioned only as a fact, not
  specified.
- **Repository hosting at scale.** No forks, no per-repo access control lists,
  no LFS, no pack-file GC policy. codefort hosts a small fleet's repos, not a
  public forge.
- **Semantic code intelligence.** Symbol indexes, call graphs, and semantic
  search are not codefort's business (see the constitution's non-goals);
  "browse" here means raw git content only.

## Checklist

- [x] Create/list/get/delete a repo over `/api/repos`
- [x] Clone, fetch, and push with a stock `git` client over HTTP
- [x] Default branch pinned to `main` on init
- [x] Browse refs, tree, blob, raw, commits, commit, compare via the read API
- [x] Path-traversal and invalid-name rejection
- [x] Delete refused while runs are in flight
- [ ] Verified against the code by the verify workflow (flip to `living`)
