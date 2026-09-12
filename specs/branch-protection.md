---
id: branch-protection
status: draft
owners: [aleh]
covers:
  - "internal/server/ci_hook.go"
  - "internal/server/git.go"
  - "internal/server/ssh.go"
  - "internal/storage/repos.go"
---
# Branch Protection

## Intent

A push that force-updates or deletes `main` is the one mistake this fleet
cannot undo from inside codefort: the old commits are still in the object
store, but nothing records what the ref used to point at, so recovery means a
reflog on somebody's laptop. Every other destructive action here is either
guarded (repo delete is type-to-confirm, merge is compare-and-swap) or
reversible (an issue reopens, a run re-runs). Ref history is neither.

So this spec adds the smallest control that closes it: a per-repo list of
protected branch patterns, and a refusal — at push time — of the two
irreversible operations against a matching branch. `VISION.md` permits "a
handful of branch-protection rules" as a coarse convenience, and that ceiling
is the design. This is not a policy engine, and it is not a permission system:
it does not care *who* pushes, only *what* the push does to history. Who may
push at all is [access-control](access-control.md)'s question, already
answered.

The precedent is the merge review gate: one per-repo setting, empty by
default, so upgrading a running deployment changes nothing until an operator
opts a repo in.

## Behavior

- WHERE a repo is created, its protected-ref list is empty, so pushes behave
  exactly as they did before this spec existed.
- WHEN an owner sets the repo's protected branch patterns, they are stored as
  an ordered list of shell-glob patterns (`main`, `release/*`) matched against
  the **branch name**, not the full ref.
- WHERE a pushed ref is not under `refs/heads/`, it is unprotected — tags and
  other refs are out of scope, because a tag's meaning here is already "a name
  someone may move".
- WHEN a push would **delete** a protected branch, the push is refused and the
  branch is left as it was.
- WHEN a push would update a protected branch to a commit that is **not a
  descendant** of its current tip — a force-push or a reset backwards — the
  push is refused.
- WHERE a push advances a protected branch by fast-forward, or creates a
  branch that does not yet exist, it is allowed: this rule guards history, not
  the act of committing.
- WHERE a push carries several refs and any one of them is refused, the
  **entire push** is rejected, because `git receive-pack` decides per-push
  before applying any ref when a pre-receive hook fails — a partial apply
  would be the surprising outcome, not the safe one.
- WHERE the refusal is reported, the client sees which branch was protected
  and why, on stderr, so the message arrives in the pusher's terminal rather
  than only in the server log.
- WHERE enforcement lives, it is a `pre-receive` hook in the bare repo, fed
  the repo's patterns through an environment variable that `codefortd` injects
  when it spawns `git receive-pack` — over HTTP and over SSH alike. The hook
  makes no network call and reads no database, so a protected branch stays
  protected whether or not the daemon can answer, and a push never waits on
  one.
- WHERE codefort itself moves a base ref during a server-side merge, it uses
  `update-ref` rather than `receive-pack` and so does not pass this hook. That
  is deliberate: a merge is already fast-forward-or-new-commit by
  construction, guarded by its own compare-and-swap, and gated by the review
  requirement when one is set.
- WHERE an operator pushes into the bare repository directly on the server's
  disk, the hook still runs but without the injected patterns, so nothing is
  protected. The operator with shell access on the box is not the mistake this
  guards against.

## Non-goals

- **Requiring a pull request.** Protecting a branch here does not mean "no
  direct pushes". A fleet that pushes to `main` all day keeps doing so; what
  it loses is the ability to rewrite what is already there. Requiring review
  before a change lands is the merge review gate's job
  ([pull-requests](pull-requests.md)), and it is a separate opt-in.
- **Per-user or per-role exceptions.** There is no bypass list, no "admins may
  force-push". A rule with an exception grid is the RBAC matrix `VISION.md`
  rules out; an operator who genuinely must rewrite history clears the pattern,
  pushes, and sets it back.
- **Required status checks.** Gating a push on a green pipeline couples the
  push path to the runner and turns a local commit into a wait. CI reports on
  what landed.
- **Pattern languages beyond shell globs.** No regular expressions, no
  negation, no precedence rules. If a fleet needs those, the answer is fewer
  branches, not a grammar.
- **Protecting tags.** Out of scope, above.

## Checklist

- [x] Per-repo protected-branch patterns, stored and settable by the owner
- [x] `pre-receive` hook refuses deletes and non-fast-forwards on a match
- [x] Patterns injected on both the HTTP and SSH push paths
- [x] Empty by default; an untouched repo pushes exactly as before
- [x] Repo settings UI for the pattern list
