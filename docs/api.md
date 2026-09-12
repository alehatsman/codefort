# HTTP API

`codefortd` serves the JSON API, git smart-HTTP, and the SPA on one port. This
document is the reference for the JSON API; the route table it describes is
`Handler()` / `apiHandler()` / `gitHandler()` in `internal/server/server.go`,
and every wire type is in `internal/api/types.go`.

## Base URL and auth

All API paths are rooted at `/api`. Everything under `/api/` requires a bearer
token:

```
Authorization: Bearer mgt_…
```

Exceptions:

- `POST /api/auth/register` and `POST /api/auth/login` — public (no token).
- `GET /healthz` — open.
- `POST /internal/ci/events` — no bearer token; loopback-only + CI-secret gated.
- Git smart-HTTP — no bearer token; gated by HTTP Basic when `CODEFORT_BASIC_USER`
  is configured.

The token's **name** is the caller's identity. Author/assignee/actor fields are
stamped server-side from it; a client-supplied `author`/`assignee` is ignored.

Rate limiting (per client IP, when `CODEFORT_RATE_LIMIT > 0`) wraps both the
authed and public `/api` surfaces and answers `429` with the standard error
envelope.

### Repo access

Every route under `/api/repos/{owner}/{repo}/…` passes through one access gate
(`internal/server/access.go`) before it reaches its handler. The rules are
coarse — see [specs/access-control.md](../specs/access-control.md):

- A **public** repo (the default for every repo) is readable and writable by any
  authenticated caller. This is the local-trust posture and is unchanged.
- A **private** repo requires the caller to be its owner or to hold a member
  row. `GET`/`HEAD`/`OPTIONS` need read access; every other method needs the
  `write` role.

Two status codes carry meaning here:

| Status | Means |
| --- | --- |
| `404` | The repo does not exist **or** the caller has no read access. Deliberately indistinguishable — a `403` would confirm a private repo exists. |
| `403` | The caller can read but not write. Existence is already no secret from them, so naming the missing grant is the useful answer. |

The principal an access check resolves is the token's **account**, not the
token's name — a session token named `alice-session` resolves to `alice`. Tokens
with no linked account (admin-provisioned, and per-run agent tokens) match by
name.

## Request and response conventions

- Requests and responses are JSON (`Content-Type: application/json` on
  responses). Exceptions: `GET …/raw` returns raw bytes, the SSE endpoints
  return `text/event-stream`, and `/healthz` returns `text/plain`.
- A malformed body is `400` with `invalid JSON: <detail>`.
- Errors use one envelope (`api.ErrorResponse`):

```json
{ "error": "issue not found" }
```

The one variant is the merge-conflict body (`api.MergeConflictResponse`), which
adds a list:

```json
{ "error": "merge conflict; resolve locally and push", "conflicts": ["a.go"] }
```

Common statuses: `400` invalid input, `401` missing/invalid token, `403` not the
owner/author, `404` unknown repo/issue/ref, `409` conflicting state, `429` rate
limited, `500` internal error.

Repo path segments accept a trailing `.git`; it is stripped.

---

## Auth & identity

### POST /api/auth/register
Self-register an account and mint its first token. Public.
- Body: `{"username", "password"}`
- `201` → `{"token": Token, "secret": "mgt_…"}` (`secret` is shown once)
- `409` username already taken; `400` on validation failure

### POST /api/auth/login
Exchange username + password for a fresh bearer token. Public.
- Body: `{"username", "password"}`
- `200` → `{"token": Token, "secret": "mgt_…"}`
- `401` invalid credentials, or the user has no password set

### GET /api/whoami
Identity of the calling token.
- `200` → `{"name": "…"}`

### GET /api/users/{username}
Public profile of a registered account.
- `200` → `User` (`id`, `name`, `created_at`)
- `404` user not found

---

## Tokens

Tokens are not owned by a user: any authenticated caller sees and manages the
full set.

### GET /api/tokens
List every token's metadata (active and revoked). Plaintext is never returned.
- `200` → `[Token]` (`id`, `name`, `created_at`, `last_used_at`, `revoked_at`)

### POST /api/tokens
Mint a token. The plaintext is returned exactly once.
- Body: `{"name"}` — required, max 100 chars
- `201` → `CreatedToken` (`Token` fields + `secret`)
- `409` token name already exists

### DELETE /api/tokens/{id}
Revoke a token; it stops authenticating immediately.
- `204` on success; `400` invalid id; `404` unknown id

---

## SSH keys

Keys are scoped to the calling token — a caller only ever sees its own.

### GET /api/ssh-keys
- `200` → `[SSHKey]` (`id`, `token_name`, `fingerprint`, `comment`,
  `created_at`, `last_used_at`)

### POST /api/ssh-keys
Register a public key against the calling token.
- Body: `{"public_key", "comment"?}` — `public_key` is one authorized_keys line
- `201` → `SSHKey`
- `400` invalid ssh public key; `409` already registered

### DELETE /api/ssh-keys/{id}
- `204`; `404` when the id is unknown **or** owned by another token

---

## Repos

### GET /api/repos
Repos visible to the caller, each with issue/PR/CI counters.
- `200` → `[Repo]` (`owner`, `name`, `open_issues`, `total_issues`,
  `ci_enabled`, `require_approval`, `protected_refs`, `ci_status`, `ci_number`,
  `open_pulls`,
  `open_reviews`,
  `active_agents`, `visibility`)

### POST /api/repos
Provision a bare git repo on disk and register it.
- Body: `{"owner", "name", "visibility"?}` — `owner`/`name` must match
  `[A-Za-z0-9._-]+` (max 100); `visibility` `public` (default) | `private`
- `201` → `Repo`
- `400` invalid owner/name; `409` repo already exists

### GET /api/repos/{owner}/{repo}
- `200` → `Repo`; `404` repo not registered

### PATCH /api/repos/{owner}/{repo}
Partial update of repo settings.
- Body: `{"ci_enabled"?, "visibility"?, "require_approval"?, "protected_refs"?}`
  — at least one required. `require_approval` is the merge review gate (see the
  merge endpoint). `protected_refs` replaces the branch-protection list whole:
  shell globs over branch names (`["main", "release/*"]`), max 32 patterns of
  200 chars; `[]` clears it. A matching branch cannot be deleted or
  force-pushed — the refusal comes from a `pre-receive` hook at push time, on
  git's stderr, not from this API.
- `200` → `Repo`
- `400` no fields, visibility not `public`/`private`, or a bad pattern (too
  many, too long, or multi-line); `404` unknown repo

### DELETE /api/repos/{owner}/{repo}
Destructive: deletes the DB row (cascading issues, runs, comments, pulls,
events), the bare git dir, and the repo's CI log tree.
- `204` on success
- `409` when the repo has in-flight runs (cancel or wait, then retry)
- `404` unknown repo

---

## Members

Listing is open to any token; add/remove is owner-only.

### GET /api/repos/{owner}/{repo}/members
- `200` → `[RepoMember]` (`username`, `role`, `joined_at`)

### POST /api/repos/{owner}/{repo}/members
- Body: `{"username", "role"?}` — `role` is `read` | `write` (default `write`)
- `204` on success
- `403` caller is not the repo owner; `404` unknown repo or user; `400` bad role

### DELETE /api/repos/{owner}/{repo}/members/{username}
- `204`; `403` not the owner; `404` repo unknown or user not a member

---

## Git data

Read endpoints share a `?ref=` param: it must name an existing **local branch**
(that validation is the injection guard), and defaults to the repo's default
branch. An unknown ref is `404 branch not found: <ref>`. Repos with no commits
return empty results rather than errors.

### GET /api/repos/{owner}/{repo}/refs
Branches, tags, and which branch is default.
- `200` → `RefList` (`default`, `branches`, `tags[]` of `{name, sha, created_at, message}`)

### GET /api/repos/{owner}/{repo}/tree
Directory listing, directories first then case-insensitive by name.
- Query: `ref`, `path` (empty = repo root)
- `200` → `Tree` (`ref`, `path`, `entries[]` of `{name, path, type, size}`)
- `400` invalid path; `404` path not found

### GET /api/repos/{owner}/{repo}/blob
One file's contents as JSON.
- Query: `ref`, `path` (required)
- `200` → `Blob` (`ref`, `path`, `size`, `binary`, `too_large`, `content`).
  `content` is empty when `binary` or `too_large` (files > 2 MiB).
- `400` path missing or a directory; `404` path not found

### GET /api/repos/{owner}/{repo}/raw
Raw file bytes, for images referenced from rendered markdown.
- Query: `ref`, `path` (required)
- `200` → the bytes, with a best-effort `Content-Type`, `X-Content-Type-Options:
  nosniff`, and `Content-Security-Policy: default-src 'none'; sandbox`
- `400` path missing or a directory; `404` path not found; `413` file > 2 MiB

### GET /api/repos/{owner}/{repo}/commits
A page of commit history, newest first.
- Query: `ref`, `path` (scope history to a path), `page` (1-based, default 1),
  `per_page` (default 30, max 100)
- `200` → `CommitList` (`ref`, `path`, `commits[]`, `has_more`)
- `400` invalid path; `404` path not found

### GET /api/repos/{owner}/{repo}/commit/{sha}
One commit plus its diff against the first parent (the empty tree for a root
commit; the first parent for a merge).
- `{sha}` must be 4–64 hex chars (full or abbreviated)
- `200` → `CommitDetail` (`commit`, `parents`, `files[]` of structured
  `DiffFile`/`DiffHunk`/`DiffLine`, `additions`, `deletions`, `truncated`).
  `truncated` is set when the diff exceeded the 20 000-line or 10 MiB budget.
- `400` invalid commit sha; `404` commit not found; `500` diff failed

### GET /api/repos/{owner}/{repo}/tree-commits
Annotates a directory listing with commit context (the latest-commit bar and
per-file "last changed" columns).
- Query: `ref`, `path`
- `200` → `TreeCommits` (`ref`, `path`, `total`, `latest`, `entries` keyed by
  full child path)
- `400` invalid path; `404` path not found

### GET /api/repos/{owner}/{repo}/compare
Three-dot (merge-base) diff of `head` relative to `base` — the read-only
foundation of the PR view. Creates nothing.
- Query: `base`, `head` — both required, both must be existing local branches
- `200` → `Compare` (`base`, `head`, `merge_base`, `ahead`, `behind`,
  `commits[]`, `files[]`, `additions`, `deletions`, `truncated`).
  `merge_base` is `""` for unrelated histories, in which case the diff is the
  whole head tree.
- `400` base/head missing; `404` branch not found; `500` diff failed

### POST /api/repos/{owner}/{repo}/branches
Create a branch from a base ref.
- Body: `{"name", "base"?}` — `base` defaults to the default branch
- `201` → `{"name", "sha"}`
- `400` name missing; `409` branch already exists; `422` base ref not found

---

## Issues

### POST /api/repos/{owner}/{repo}/issues
- Body: `{"title", "body"?, "parent"?, "labels"?}` — `title` required (trimmed);
  `parent` is a parent issue number
- `201` → `Issue`
- `400` empty title, negative parent, or parent not in this repo

### GET /api/repos/{owner}/{repo}/issues
- Query: `state` (repeatable / comma-separated: `todo`, `in_progress`, `done`,
  `closed`), `assignee`, `author`, `q` (keyword), `label`, `sort`
  (`newest` default | `oldest` | `recently-updated`), `limit`, `offset`,
  and the flags `ready`, `blocked`, `epics` (bare flag = true; `false`/`0` off)
- `q` is a case-insensitive substring search over title and body. Whitespace
  separates terms and every term must match (order and adjacency are ignored);
  terms past the eighth are dropped, not rejected. With `q` set and `sort`
  omitted, rows are ranked by how many terms hit the *title* before falling back
  to newest-first; passing `sort` explicitly disables the ranking.
- `ready` and `blocked` are mutually exclusive; `epics` cannot be combined with
  either — both are `400`
- `200` → `[Issue]`, plus an `X-Total-Count` header with the unpaged total.
  With `epics`, each row carries a `progress` child rollup.
- `400` invalid state/sort/limit/offset or an illegal flag combination

### GET /api/repos/{owner}/{repo}/issues/{number}
- `200` → `Issue`, enriched with `children`, `progress`, `depends_on`, `blocks`
- `400` invalid issue number; `404` issue not found

### PATCH /api/repos/{owner}/{repo}/issues/{number}
Partial update; only non-nil fields change.
- Body: `{"state"?, "title"?, "body"?, "parent"?, "labels"?}` — `parent` 0
  clears the parent; `labels` (even `[]`) replaces the set
- `200` → `Issue`
- `400` invalid state, empty title, bad parent, or no fields to update;
  `404` issue not found

### DELETE /api/repos/{owner}/{repo}/issues/{number}
Any valid token may delete.
- `204`; `400` invalid number; `404` issue not found

### POST /api/repos/{owner}/{repo}/issues/{number}/claim
Atomically take an unassigned issue. Assignee is the token name.
- Body: `{"state"?}` — optional transition applied with the claim
- `200` → `Issue`
- `400` invalid state; `404` issue not found; `409` already claimed

### POST /api/repos/{owner}/{repo}/issues/{number}/unclaim
- `200` → `Issue`
- `403` claimed by another identity; `404` issue not found

### GET /api/repos/{owner}/{repo}/issues/{number}/commits
Commits whose message references `#N` on a non-digit boundary, across all
branches, newest first. Each carries a `branch` hint.
- `200` → `[Commit]` (empty for an unborn repo or no match)
- `400` invalid issue number

### POST /api/repos/{owner}/{repo}/issues/{number}/agent
See [Agent runs](#agent-runs).

---

## Comments

Issue comments. Path resolves `{owner}/{repo}/issues/{number}`.

### GET /api/repos/{owner}/{repo}/issues/{number}/comments
- `200` → `[Comment]` (`id`, `issue_id`, `author`, `body`, `created_at`)

### POST /api/repos/{owner}/{repo}/issues/{number}/comments
- Body: `{"body"}` — required, trimmed; author is stamped from the token
- `201` → `Comment`; `400` empty body

### DELETE /api/repos/{owner}/{repo}/issues/{number}/comments/{comment_id}
Author-only.
- `204`; `400` invalid comment id; `403` not the author; `404` comment not found

---

## Dependencies

A depends-on edge means the path issue is blocked until the target reaches a
done/closed state. Both mutations return the full issue with its edge sets
(`children`, `depends_on`, `blocks`), the same shape as `GET`.

### POST /api/repos/{owner}/{repo}/issues/{number}/dependencies
- Body: `{"depends_on": <issue number>}` — must be positive
- `200` → `Issue`
- `400` self-edge, non-positive target, or issue not in this repo;
  `409` the edge would create a cycle

### DELETE /api/repos/{owner}/{repo}/issues/{number}/dependencies/{target}
Idempotent — removing a non-existent edge succeeds.
- `200` → `Issue`; `400` invalid number or target

---

## Pull requests

### POST /api/repos/{owner}/{repo}/pulls
- Body: `{"base", "head", "title", "body"?}` — all trimmed; `base` and `head`
  must be existing local branches and must differ
- `201` → `PullRequest`
- `400` missing base/head/title or base == head; `404` branch not found

### GET /api/repos/{owner}/{repo}/pulls
- Query: `state` (repeatable / comma-separated: `open`, `merged`, `closed`),
  `q` (title/body keyword)
- `200` → `[PullRequest]`; `400` invalid state

### GET /api/repos/{owner}/{repo}/pulls/{number}
PR plus the head-vs-base compare, code-review comments on the head branch
(resolved ones included), and reviews. For a merged PR the compare is
reproduced from the base/head tips frozen at merge time; for an open PR it's
the live branches. The compare is best-effort — if the commits or branches are
gone it degrades to base/head with an empty diff.
- `200` → `PullRequestDetail` (`PullRequest` + `compare`, `comments`, `reviews`)
- `400` invalid number; `404` pull request not found

### PATCH /api/repos/{owner}/{repo}/pulls/{number}
- Body: `{"title"?, "body"?, "state"?}` — `state` may move to `closed` or back
  to `open`
- `200` → `PullRequest`
- `400` invalid state, `merged` (use the merge endpoint), empty title, or no
  fields to update; `404` pull request not found

### POST /api/repos/{owner}/{repo}/pulls/{number}/merge
Merges head into base inside the bare repo (worktree-free `merge-tree` +
`commit-tree` + CAS `update-ref`). Conflicts are reported, never auto-resolved.
On success, issues referenced by closing keywords (`closes/fixes/resolves #N`)
in the PR title+body are moved to `done`, and the base branch is pushed to the
`mirror` remote if one is configured (best-effort, async).
- Body (optional; empty body allowed): `{"method"?}` — `merge` (default) |
  `ff-only` | `rebase`. `rebase` replays each non-merge commit the head adds
  over base onto the base tip (authorship preserved, committer `codefort`) and
  advances base to the last one, so the result is linear and `fast_forward` is
  true. It does not move the head ref — the replayed commits are new objects.
  An already-linear head fast-forwards instead of being replayed.
- `200` → `MergeResult` (`PullRequest` + `merge_commit`, `fast_forward`)
- `400` invalid method / invalid number
- `404` pull request not found
- `409` review gate (when the repo has `require_approval`): no approving review,
  or an outstanding `changes_requested` — checked before any ref is touched ·
  PR not open · base or head branch no longer exists · not
  fast-forwardable (`ff-only`) · nothing to rebase, i.e. the head adds only
  merge commits (`rebase`) · base moved during the merge (retry) ·
  **merge conflict** — body is `MergeConflictResponse` with `conflicts[]`. A
  conflicting rebase leaves base exactly where it was; commits are only
  published once every one of them replays cleanly.

### POST /api/repos/{owner}/{repo}/pulls/{number}/reviews
Upsert the caller's verdict on a PR.
- Body: `{"state"}` — `approved` | `changes_requested`
- `200` → `PRReview` (`id`, `author`, `state`, `updated_at`)
- `400` invalid number or state

---

## Code comments

Comments anchored to a 1-based inclusive line range of a file on a branch. Also
the backing store for PR review threads (anchored to the PR's head branch).

### GET /api/repos/{owner}/{repo}/code-comments
- Query: `ref` (branch, default = default branch), `path` (scope to one file),
  `state` — `open` (default) | `resolved` | `all`
- `200` → `[CodeComment]`, each with `snippet` filled in from the source lines
  at `ref` (empty when the file or range is gone)
- `400` invalid path; `404` branch not found

### POST /api/repos/{owner}/{repo}/code-comments
- Body: `{"ref"?, "path", "start_line", "end_line", "body"}` — `ref` defaults to
  the default branch; `start_line >= 1` and `end_line >= start_line`; `author`
  and `commit_sha` (the ref's HEAD) are stamped server-side. Max body 1 MiB.
- `201` → `CodeComment`
- `400` empty body, missing path, or invalid line range;
  `404` branch not found, or path not a file on that branch

### PATCH /api/repos/{owner}/{repo}/code-comments/{id}
Toggle the resolved flag. Author-only; only `resolved` is mutable.
- Body: `{"resolved": bool}` — required
- `200` → `CodeComment`
- `400` invalid id or missing `resolved`; `403` not the author; `404` not found

### DELETE /api/repos/{owner}/{repo}/code-comments/{id}
Author-only.
- `204`; `400` invalid id; `403` not the author; `404` not found

---

## CI runs

Runs are addressed by their per-repo `number`. `kind` is `ci` or an agent-family
kind; the internal DB id is never exposed.

### GET /api/repos/{owner}/{repo}/runs
Newest first. Filters are applied in SQL, so `limit` caps the matching set.
- Query: `kind` (`ci` | `agent`), `state` (comma-separated status list, e.g.
  `running,queued`), `q` (keyword over commit subject/author, ref, trigger),
  `limit`
- `200` → `[CIRun]`; `400` invalid limit

### POST /api/repos/{owner}/{repo}/runs
Trigger a run for an arbitrary ref without a push; the run's `event` is
`manual`. Max body 64 KiB.
- Body: `{"ref"}` — branch, tag, or commit SHA; may not start with `-`
- `202` → `CIRun`
- `400` missing/invalid ref, or the ref can't be resolved to a commit;
  `409` CI is disabled for this repo

### GET /api/repos/{owner}/{repo}/runs/{number}
- `200` → `CIRunDetail` (`CIRun` + `jobs[]`; agent-family runs also carry
  `turns[]`, the follow-up conversation — the issue body is turn 1 and is not
  listed)
- `400` invalid run number; `404` run not found

### POST /api/repos/{owner}/{repo}/runs/{number}/rerun
Re-enqueues a fresh run for the source run's commit/ref under a new run number;
history is append-only.
- `202` → `CIRun`
- `404` run not found; `409` CI is disabled for this repo

### GET /api/repos/{owner}/{repo}/runs/{number}/jobs/{job}/events
SSE — see [Streaming endpoints](#streaming-endpoints).

### POST /api/repos/{owner}/{repo}/runs/{number}/cancel
Force-stop a non-terminal run of either kind: interrupts the in-flight work and
discards the workspace. Requires the in-process runner.
- `202` → `CIRun` (status `canceled`)
- `404` run not found; `409` run is already finished;
  `503` cancel unavailable (no runner wired in)

---

## Agent runs

Agent runs ride the same run/job spine as CI, so their progress streams over the
same endpoints.

### POST /api/repos/{owner}/{repo}/issues/{number}/agent
Spawn an agent run for an issue. Body optional (max 64 KiB).
- Body: `{"ref"?, "model"?, "tool_profile"?}` — `ref` defaults to `HEAD` and may
  not start with `-`; `model` falls back to the operator default then the
  built-in default; `tool_profile` is `full` (default) | `review` (read-only)
- `202` → `CIRun` (`kind: agent`, `event: agent`, `issue_number` set)
- `400` invalid issue number, unresolvable ref, invalid execution model, or
  invalid tool profile; `404` issue not found

### POST /api/repos/{owner}/{repo}/runs/{number}/turns
Queue a follow-up message on a live agent run; it queues behind any in-flight
turn. Max body 1 MiB.
- Body: `{"text"}` — required, trimmed
- `202` → `AgentTurn` (`seq`, `author`, `body`, `status`, `created_at`,
  `finished_at`)
- `400` invalid run number, empty text, or not an agent run;
  `404` run not found; `409` agent run has finished

### POST /api/repos/{owner}/{repo}/runs/{number}/finish
Accept a parked agent run: transitions `awaiting_input` → `finishing`, after
which the runner materializes the branch and posts the summary comment.
- `202` → `CIRun` (status `finishing`)
- `400` not an agent run; `404` run not found;
  `409` run is not awaiting input (busy or already finished)

---

## Settings

Global, operator-facing agent config. Secrets are write-only: the API reports
only whether one is set.

### GET /api/settings/agent
- `200` → `AgentSettings` (`claude_oauth_token_set`,
  `claude_token_env_fallback`, `execution_model`, `llm_base_url`,
  `anthropic_auth_token_set`)

### PUT /api/settings/agent
Per field: absent/null leaves it unchanged, `""` clears it, any other value
sets it. Max body 64 KiB.
- Body: `{"claude_oauth_token"?, "execution_model"?, "llm_base_url"?,
  "anthropic_auth_token"?}`
- `200` → `AgentSettings`
- `400` invalid request body or invalid execution model

---

## Specs

In-repo markdown specs under `specs/`. See [specs.md](specs.md) for the file
format. Read endpoints take `?ref=` like the other git-data reads.

### GET /api/repos/{owner}/{repo}/specs
Every `.md` under `specs/` on the ref with parsed frontmatter, sorted by path. A
repo with no `specs/` dir (or no commits) returns an empty list, not a 404. A
spec with malformed frontmatter is still listed, with a path-derived title.
- Query: `ref`
- `200` → `SpecList` (`ref`, `specs[]` of `{path, id, title, status, owners,
  covers, last_verified, alignment}`)

### GET /api/repos/{owner}/{repo}/specs/{path...}
One spec's content and parsed structure. `{path...}` is relative to `specs/`
(`…/specs/ci/pipeline.md` reads `specs/ci/pipeline.md`).
- Query: `ref`
- `200` → `SpecContent` (metadata + `content` raw, `body` without frontmatter,
  `sections[]`, `checklist[]`, and `verification` — the latest verify pass with
  `alignment`, `markers[]`, `conflicts`, `verified_at`, `commit`, `stale`)
- `400` path missing or a directory; `404` spec not found; `413` spec > 2 MiB

### PUT /api/repos/{owner}/{repo}/specs/{path...}
Commit spec content to a feature branch (worktree-free: `hash-object` →
temp-index tree → `commit-tree` → CAS `update-ref`). Never writes to the
default branch — specs land via a PR. The commit is stamped with the token's
identity.
- Body: `{"content", "message"?, "branch"?, "base"?}` — `branch` defaults to
  `spec/<filename-stem>`; `base` (used only when the branch is created)
  defaults to the default branch; `message` defaults to
  `docs(specs): update <path>`
- `200` → `WriteSpecResult` (`branch`, `commit`, `created`)
- `400` path missing, not `.md`, or `branch` is the default branch
- `404` base branch not found
- `409` spec content is unchanged, or the branch moved during the write (retry)

### GET /api/repos/{owner}/{repo}/specs/drift
Deterministic (non-LLM) drift status per spec: did the code its `covers[]`
globs govern change since the last verification? The `unverified` + `stale` set
is the candidate set for a verify pass.
- Query: `ref`
- `200` → `SpecDriftReport` (`ref`, `specs[]` of `{path, id, status, covers,
  base, changed, last_verified}`); `status` is `uncovered` | `unverified` |
  `stale` | `fresh`

### POST /api/repos/{owner}/{repo}/specs/verify
Enqueue a spec-verify agent run (read-only tool profile) for one spec. It
streams over the same run/job endpoints as any agent run.
- Body: `{"path", "ref"?}` — `path` must be a `.md` file under `specs/`; `ref`
  defaults to the default branch and may not start with `-`
- `202` → `CIRun` (`event: spec-verify`)
- `400` bad path, or the ref can't be resolved to a commit
- `404` spec not found at that commit

---

## Events

### GET /api/events
SSE fleet event feed — see [Streaming endpoints](#streaming-endpoints).

### POST /internal/ci/events
Push notification from the bare repo's `post-receive` hook, one request per
pushed ref. **Not** on the bearer surface: loopback-only and gated by the
per-process secret in `X-Moongit-CI-Secret`. Max body 64 KiB.
- Body: `{"repo": "owner/name", "old", "new", "ref", "pusher"}`
- Emits a `push` event, auto-closes open PRs whose head reached base via the
  push, supersedes active CI runs for the ref, then enqueues a run.
- `202` → `{"run": <number>}`
- `204` when nothing was enqueued: branch delete (zero `new` SHA), CI disabled
  for the repo, or the pipeline's `on.push.branches` filter excludes the ref
- `400` bad JSON or bad repo slug; `403` non-loopback or bad secret;
  `404` unknown repo; `405` non-POST

### GET /healthz
Liveness + writer-pool probe (bounded by a 2 s timeout). Open, no auth.
- `200` `ok`; `503` `db unavailable` — both `text/plain`

---

## Cross-repo aggregates

Fleet-wide feeds behind the top-level (non-repo) list views. Each row is tagged
with its owning `repo` (`{owner, name}`); `number` stays per-repo.

### GET /api/issues
Issues across every repo, newest-updated first. `sort` is ignored here — the
cross-repo feed is always by recency, behind the same title-hit ranking the
per-repo list applies when `q` is set.
- Query: same as the per-repo issue list (`state`, `assignee`, `author`, `q`,
  `label`, `limit`, `offset`, `ready`, `blocked`, `epics`)
- `200` → `[IssueWithRepo]`, plus `X-Total-Count`; `400` invalid filter

### GET /api/pulls
- Query: `state`, `q`
- `200` → `[PullRequestWithRepo]`; `400` invalid state

### GET /api/runs
- Query: `kind`, `state`, `q`, `limit`
- `200` → `[CIRunWithRepo]`; `400` invalid limit

---

## Streaming endpoints

Two endpoints speak Server-Sent Events. Both frame each message as
`id: <seq>` / `event: <type>` / `data: <json>`, send `: ping` heartbeats, and
set `Cache-Control: no-cache` and `X-Accel-Buffering: no`. Both resume from
the `Last-Event-ID` request header, falling back to a `?last_event_id=` query
param for clients that can't set headers; only events with a **higher** seq are
sent. An unparseable or absent value replays from the start.

### GET /api/events
The outbound fleet feed. Replays the events table from the resume point, then
live-tails new rows until the client disconnects. Long-lived — it never
self-closes.
- Query: `repo=owner/name` (scope to one repo), `types=a,b` (filter by event
  type; empty = all), `once=true` (replay the available backlog and close,
  instead of tailing), `last_event_id`
- Data payload: `Event` (`seq`, `type`, `time` in unix ms, `repo`, `actor`,
  `data`). Types are dotted — `issue.created`, `issue.claimed`,
  `issue.state_changed`, `issue.updated`, `issue.unclaimed`, `issue.commented`,
  `pull.opened`, `pull.merged`, `pull.closed`, `review.submitted`,
  `ci.run.queued`, `ci.run.finished`, `repo.deleted`, `push`.
- `400` `repo` not in `owner/name` form; `404` repo not registered;
  `500` streaming unsupported

### GET /api/repos/{owner}/{repo}/runs/{number}/jobs/{job}/events
A job's event log. Replays the on-disk `events.jsonl` from the resume point,
then live-tails while the run is in flight.

The response is deliberately finite. It ends when the run reaches a terminal
state or parks at `awaiting_input` (after a final drain), and it also ends
every ~2 s while a run is live — emitting a bare `event: resume` sentinel
first — so a buffering hop (SSH tunnel, reverse proxy) flushes the transcript.
A `resume` sentinel means *reconnect from the last id*, not end-of-stream.

- Query: `last_event_id`
- `400` invalid run number; `404` run not found, or the job doesn't exist and
  the run is already terminal (a not-yet-created job on a live run streams and
  waits, as long as the name is a safe single path component);
  `500` streaming unsupported

---

## Git smart-HTTP

Served off the API mux, under `/{owner}/{repo}/…`. `{repo}` may carry a
trailing `.git` (real git clients send it). No bearer token; HTTP Basic gates
these when `CODEFORT_BASIC_USER` is set.

### GET /{owner}/{repo}/info/refs
- Query: `service` — must be `git-upload-pack` or `git-receive-pack`
- `200` → pkt-line ref advertisement,
  `Content-Type: application/x-<service>-advertisement`
- `400` bad owner/repo; `403` unsupported service; `404` repo not on disk

### POST /{owner}/{repo}/git-upload-pack
Fetch/clone RPC.
- `Content-Type: application/x-git-upload-pack-request` (gzip bodies accepted)
- `200` → `application/x-git-upload-pack-result`
- `400` bad owner/repo; `404` repo not on disk; `415` unexpected content-type

### POST /{owner}/{repo}/git-receive-pack
Push RPC. On push the server injects `CODEFORT_CI_URL`, `CODEFORT_CI_SECRET`,
`CODEFORT_CI_REPO` and `CODEFORT_CI_PUSHER` (the Basic-auth user) into
`receive-pack`'s environment, which the `post-receive` hook inherits to notify
`/internal/ci/events`.
- `Content-Type: application/x-git-receive-pack-request` (gzip bodies accepted)
- `200` → `application/x-git-receive-pack-result`
- `400` bad owner/repo; `404` repo not on disk; `415` unexpected content-type
