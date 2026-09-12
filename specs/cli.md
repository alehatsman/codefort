---
id: cli
status: draft
owners: [aleh]
covers:
  - "cmd/cf/main.go"
  - "cmd/cf/pr.go"
  - "cmd/cf/events.go"
---
# mgit — the human-facing client

## Intent

`mgit` (built from `cmd/cf`) is the hand-driven front door to a moongit
server: a thin, stateless REST client that infers *where to talk* from the git
checkout you are standing in, so the common case is a bare verb — `mgit issue
claim 42` — with no host, repo, or user to type. It holds no config file, no
cache, and no local state; every invocation resolves its target from `git
remote`, authenticates with one environment variable, makes one (or a few)
authenticated `/api` calls, prints a human-readable summary, and exits. The
server owns all semantics — validation, identity stamping, claim conflicts — and
the client's job is to *not* add a second source of truth: it validates only
what it can settle locally (arg shape, enum spelling, mutually exclusive flags)
so a typo costs no round-trip, and otherwise surfaces the server's own message
verbatim. This is deliberately the terse, dogfooded surface; the agent-facing
dual is `mgit mcp` (see mcp-server), which reuses exactly this plumbing.

## Behavior

### Target resolution and identity

- WHEN any server-touching command runs, the target is resolved from the current
  checkout: the `moongit` remote is tried first (the code mirror), and only if
  that remote is absent does it fall back to `origin`. The first one found wins
  outright — a malformed `moongit` remote is an error, not a reason to try
  `origin`.
- WHERE the chosen remote is `http(s)://host/owner/repo(.git)`, both the server
  base URL and `owner/repo` come from it; WHERE it is `ssh://…` or scp-like
  (`git@host:owner/repo.git`), only `owner/repo` is derived and the server base
  is left empty. Any other scheme (e.g. `file://`) is rejected.
- WHERE `MOONGIT_SERVER` is set, it overrides **only** the server base URL
  (trailing slashes trimmed); `owner/repo` always comes from the remote. It is
  therefore required for an SSH-only checkout, and never a substitute for having
  a remote at all.
- IF neither remote exists, the command fails telling the user to run inside a
  checkout of the target repo; IF a remote resolved but no http base could be
  derived and `MOONGIT_SERVER` is unset, it fails asking for one.
- WHERE a request is authenticated, `MOONGIT_TOKEN` is sent as an
  `Authorization: Bearer` header and is the client's *only* notion of identity.
  IF the variable is unset the header is simply omitted and the request still
  goes out — the resulting 401 is reported as a plain server error, not caught
  client-side.
- WHILE any write happens, the client sends no author, assignee, or actor field:
  the server stamps them from the token's name. There is no `--author` flag on
  any command (see the drift note in the checklist).

### Command groups

- WHEN `mgit` is run with no arguments, or with `help`/`-h`/`--help`, it prints
  usage to stdout and exits 0; an unrecognised top-level or sub-command is an
  error naming the offender.
- WHERE a command takes an identifier (issue number, PR number, comment id,
  `owner/name`), that identifier is **positional and must precede the flags** —
  Go's flag parser stops at the first non-flag argument, so the client parses
  the positional itself and hands only the remainder to the flag set. Leftover
  positionals after the flags are rejected as "unexpected extra args".
- WHERE `issue` is used (`create`/`list`/`show`/`edit`/`set-state`/`claim`/
  `unclaim`/`delete`/`comment`), it wraps the issue data plane: `create`
  requires `--title`; `comment` requires a non-empty `--body`; `set-state` takes
  the state as a second positional; states and the mutually exclusive
  `--ready`/`--blocked`/`--epics` list views are validated locally against the
  shared API enums before any request.
- WHEN `issue edit` runs, only the flags actually passed are sent, so editing a
  title never clobbers the body — an explicitly-empty `--body ""` is
  distinguishable from an omitted one, and `--labels ""` clears all labels.
  Requesting no change at all is a usage error.
- WHILE `issue edit` applies dependency edges, it is a *sequence* of calls — one
  PATCH, then a DELETE per `--remove-depends-on`, then a POST per
  `--depends-on`, then a re-fetch so the printed line reflects the final state.
  A failure mid-sequence aborts and leaves the earlier steps applied.
- WHERE `review` is used (`list`/`create`/`resolve`/`reopen`/`delete`), it wraps
  code comments anchored to a file's line range on a branch. `create` requires
  `--path`, `--lines`, and `--body`; `--lines` accepts `N` or a non-decreasing
  1-based range `A-B`, mirroring the `L5` / `L5-L12` form `list` prints.
- WHERE `pr` is used (`create`/`list`/`show`/`merge`/`close`/`reopen`), it wraps
  pull requests: `create` requires `--base`, `--head`, `--title`; `merge`
  defaults to a merge commit and takes `--ff-only` for the strict path;
  `close`/`reopen` flip state over PATCH, and merging is reachable only through
  `merge` — the client never asks the server for the `merged` state directly.
- WHEN a merge returns a conflict carrying file paths, the client prints the
  conflicting paths and tells the user to resolve locally and retry; any other
  conflict (not fast-forwardable, nothing to merge, already merged) surfaces the
  server's message as-is.
- WHERE `ci validate` is used, it is purely local: it parses an `mgitci.yml`
  (defaulting to `./mgitci.yml`) with the same parser the server uses, prints the
  jobs and their `needs:` edges, and warns where a job invokes a toolchain the
  default image does not carry. It resolves no target and needs no token — it is
  the authoring-time check before pushing.
- WHERE `ci run <ref>` is used, it is the only `ci` subcommand that talks to the
  server: it enqueues an on-demand run for a branch, tag, or SHA and prints the
  queued run number; a conflict is reported as CI being disabled for the repo.
- WHERE `repo delete <owner>/<name>` is used, the repo comes from the explicit
  argument, not the checkout — only the server URL is resolved from the remote —
  so it works from any checkout. A checkout (or `MOONGIT_SERVER` plus a remote)
  is still required.
- WHERE a command is destructive (`issue delete`, `repo delete`), it prompts on
  stdout for a `y`/`N` confirmation naming exactly what will be destroyed, unless
  `--yes` (or `-y`) is passed. Declining prints `aborted` and exits **0** — a
  refused confirmation is not an error.
- WHERE `mgit mcp` appears in dispatch, it is this CLI's stdio/MCP sibling and is
  specified separately; it reuses target resolution and `MOONGIT_TOKEN` unchanged.

### Streaming the feed

- WHEN `mgit events` runs, it opens the authenticated SSE feed at `/api/events`
  on the resolved server (cross-repo by default), prints one compact line per
  event — `HH:MM:SS  type  repo  @actor  summary`, with a per-type summary pulled
  from the event payload — and tails until interrupted.
- WHERE `--repo owner/name` or `--types a,b` are given, they are passed to the
  server as query filters; the client does no filtering of its own.
- WHILE tailing, resumption is by sequence number: `--since <seq>` seeds the
  cursor, each connection sends the highest seq seen as `Last-Event-ID`, and the
  cursor advances from the decoded event payload — so a reconnect replays from
  after the last line printed, not from the start.
- IF the stream ends cleanly or drops on a transport error, the client notes the
  drop on stderr and reconnects after one second, indefinitely; IF the server
  answers a non-OK status (401, unknown-repo 404), that is non-retryable and the
  command exits with the error.
- WHERE `--once` is given, the client asks the server to close after draining the
  backlog and exits 0 once drained — the scriptable, non-tailing mode.

### Output and exit status

- WHERE output is produced, all command results go to **stdout** as
  human-formatted lines; only the terminal error and the events reconnect notice
  go to **stderr**.
- WHEN a command fails for any reason, the error is printed to stderr prefixed
  `moongit:` and the process exits **1**. There is exactly one failure exit code:
  a usage error, a not-found, and a claim conflict are indistinguishable by
  status and must be told apart by parsing the message.
- WHERE a request fails, the client prefers the server's own `error` field over
  the raw body, and a few cases get a friendlier gloss instead of the status
  line: a claim conflict reads "issue #N already claimed", a missing repo names
  the `owner/name`, a review anchor 404 says branch-or-path not found.
- WHERE machine-readable output is concerned, `review list --json` is the **only**
  command that emits raw API JSON; every other command prints formatted text
  only, so scripts and agents must scrape columns. This is a real gap for the
  fleet's scripted use (agents are expected to drive `mgit`), tracked below.

## Non-goals

- **The MCP tool surface.** `mgit mcp`'s toolset, profiles, and stdio transport
  are the mcp-server spec's; this spec covers it only as one dispatch branch that
  shares target resolution and auth.
- **Server-side semantics of the endpoints being called.** What a claim locks,
  how a PR merges, how a pipeline schedules, what the feed guarantees — issues,
  pull-requests, ci-pipelines, and events-feed own those. Here the client is a
  transport: it states *which* endpoint a verb hits and how the result is
  rendered, never what the server decides.
- **The `moongitd` server binary's CLI.** `moongitd serve`, `token create`,
  `repo create` and friends are the operator-side surface — a different binary,
  a different audience, and out of scope here even though `token create` mints
  the `MOONGIT_TOKEN` this client consumes.
- **Git itself.** `mgit` never wraps clone/push/fetch; it reads `git remote` to
  locate the server and nothing more. Code moves over plain git.
- **Configuration and sessions.** No config file, no login/logout, no profile or
  context switching, no credential storage: two environment variables and the
  checkout you are standing in are the entire input surface. Multi-repo work is
  done by changing directory.

## Checklist

- [x] Target resolved from the `moongit` remote, falling back to `origin`
- [x] http(s) remotes yield server + owner/repo; ssh/scp yield owner/repo only
- [x] `MOONGIT_SERVER` overrides the server base URL only, never owner/repo
- [x] `MOONGIT_TOKEN` sent as Bearer; identity stamped server-side; no author sent
- [x] issue / review / pr / ci / repo / events / mcp / help dispatch with local arg + enum validation
- [x] Positional identifier before flags; trailing positionals rejected
- [x] `issue edit` sends only the flags passed; distinguishes empty from omitted
- [x] `ci validate` is fully local (no target, no token); `ci run` triggers server-side
- [x] `repo delete` targets an explicit `owner/name`, server URL from the remote
- [x] `issue delete` / `repo delete` confirm interactively unless `--yes`/`-y`; declining exits 0
- [x] `events` filters via `--repo`/`--types`, resumes by seq/`Last-Event-ID`, reconnects on drop, `--once` drains and exits
- [x] Results on stdout, errors on stderr prefixed `moongit:`; server error messages surfaced verbatim
- [ ] **No `--json` anywhere except `review list`** — agents scripting `mgit` must parse formatted text
- [ ] **One exit code for every failure (1)** — usage vs not-found vs conflict indistinguishable programmatically
- [ ] `issue edit` multi-call sequence is not atomic: a mid-sequence failure leaves earlier steps applied
- [ ] Usage text drift: `issue comment`'s usage advertises `--author`, which no flag set defines; `--labels`/`--label`, `pr close`/`pr reopen`, `mcp --profile`, and the `-y` shorthand are absent from `mgit help`
- [ ] Usage text says to run inside a checkout whose `origin` points at moongit, but resolution prefers a `moongit` remote
- [ ] Usage and error text say `moongit`; the distributed binary is `mgit`
- [ ] No `--repo` override: every server-touching command, including `repo delete`, needs a git checkout with a usable remote
- [ ] Verified against the code by the verify workflow (flip to `living`)
