---
id: pull-requests
status: draft
owners: [aleh]
covers:
  - "internal/server/pulls.go"
  - "internal/server/merge.go"
  - "internal/server/code_comments.go"
  - "internal/storage/pulls.go"
  - "internal/storage/code_comments.go"
---
# Pull Requests & Review

## Intent

A pull request proposes folding one branch into another, and merging it is how a
repo's canonical branch actually advances on the server. moongit performs the
merge inside its own bare repository — no worktree, no checkout — so it is safe
against the same repo that smart-HTTP is concurrently serving, and the result is
a real ref update, not a relabeled row. Review happens through comments anchored
to specific lines of code on a branch. The posture is local-trust: any valid
token may open, review, and merge; correctness comes from git's own ancestry and
conflict rules, not from access gates.

## Behavior

- WHEN a client opens a PR, both base and head must name existing, distinct local
  branches and a title must be given; the author is stamped from the token.
- WHEN a client fetches a PR, the response embeds the head-vs-base compare and
  the review comments anchored to the head branch.
- WHERE a PR is merged, its compare is reproduced from the base/head tips frozen
  at merge time (a live compare would be empty once head is folded into base);
  for an open PR the live branches are compared, best-effort so a PR whose branch
  was deleted still loads.
- WHEN a client updates a PR, title, body, and state may change; state may move to
  closed (abandon) or back to open (reopen), but cannot be set to merged — merging
  only happens through the merge endpoint, so refs are actually joined.
- WHEN a client merges an open PR, the server joins head into base inside the bare
  repo and marks the PR merged, returning the resulting commit and whether it was
  a fast-forward.
- IF the PR is not open, or either branch no longer exists, or head is already
  contained in base, the merge is refused with a conflict.
- WHERE the merge method is fast-forward-only, the merge succeeds only if base is
  an ancestor of head; otherwise it is refused as not fast-forwardable.
- WHERE the merge method is a merge commit (the default), the server computes the
  merged tree in memory; on a content conflict it reports the conflicting paths
  and refuses, never auto-resolving — the author resolves locally and pushes.
- WHILE a merge updates the base ref, the move is compare-and-swap against the
  ref's pre-merge tip, so a concurrent push that moved base is detected and the
  merge is refused as retryable rather than clobbering the push.
- WHEN a merge commit is created, it is stamped with the merging token's identity
  (synthetic email; moongit identifies by token name, not email).
- WHEN a PR is merged on the server, the server enqueues a CI run for the
  resulting base tip — the same gate and run lifecycle as a pushed commit — and
  emits a merge event to the fleet feed, so a merge is built, tested, and visible
  like any other change to the branch.
- WHERE the bare repo has a git remote named `mirror`, a successful merge also
  pushes the updated base branch to it, so an external mirror (e.g. GitHub) tracks
  the canonical server without a manual sync. The push is best-effort and
  asynchronous — the server is the source of truth and a mirror failure never
  fails or delays the merge — and plain (never forced), so a diverged mirror is
  reported rather than clobbered. A repo with no `mirror` remote is unaffected.
- WHEN a PR's head is not fast-forwardable onto base, a client can have the server
  rebase head onto base inside the bare repo (worktree-free), reporting conflicts
  the same way a merge does, so a linear history is produced without a local
  fetch-rebase-push round-trip.
- WHEN a client creates a review comment, it is anchored to a file line range on a
  branch (defaulting to the repo's HEAD branch); the branch and path must exist,
  the body is required, and the branch's current commit is frozen for drift
  context.
- WHEN review comments are listed, each carries a snippet of the referenced source
  lines so a reviewer sees the code without a second fetch; a comment whose code
  has since moved or vanished simply renders without a snippet.
- WHILE a review comment exists, only its author may resolve/unresolve or delete
  it; listing can scope to open (default), resolved, or all.

## Non-goals

- **Issues.** Issues and their claim-first coordination are a separate spec; a PR
  references work but is not the work-lock.
- **CI run execution.** A server-side merge *triggers* a CI run (in Behavior
  above); how that run is gated, isolated, and streamed is the ci-pipelines spec.
- **The events feed.** A merge emits an event, but the SSE fleet feed's delivery
  and backing store are the events-feed spec's concern.
- **Diff/compare computation.** Producing the commit/file diff is the shared
  code-browse machinery (git-hosting); here the compare is consumed, not
  specified.
- **Branch protection, required reviews, approvals.** No merge gating, no
  draft/auto-merge, no CODEOWNERS. Local-trust: the merge is allowed when git
  says it is mergeable, full stop.

## Checklist

- [x] Open a PR between two existing, distinct branches; token-stamped author
- [x] PR detail embeds head-vs-base compare + anchored review comments
- [x] Merged-PR compare reproduced from frozen pre-merge tips
- [x] Update title/body/state (close/reopen); merged only via merge endpoint
- [x] Merge inside the bare repo (no worktree), returns commit + fast-forward flag
- [x] ff-only vs merge-commit methods; refuse non-fast-forward under ff-only
- [x] Conflicts reported as paths, never auto-resolved
- [x] CAS ref update guards against a concurrent push moving base
- [x] Review comments anchored to a line range, with source snippets
- [x] Author-only resolve/unresolve and delete; open/resolved/all listing
- [ ] CI run + merge event enqueued on a server-side merge (#256)
- [x] Merged base branch mirror-pushed to a configured `mirror` remote (best-effort, non-forced)
- [ ] Server-side rebase of head onto base, worktree-free (#257)
- [ ] Verified against the code by the verify workflow (flip to `living`)
