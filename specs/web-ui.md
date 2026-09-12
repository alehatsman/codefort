---
id: web-ui
status: draft
owners: [aleh]
covers:
  - "web/src/**"
---
# Web UI

## Intent

The SPA is the human face of the same `/api` surface `cf` drives — served by
the same `codefortd` binary on the same port, so self-hosting the daemon is all
it takes to get a UI. It is a thin, pull-based client: the server owns all
state, the browser owns a token and a cache. Its job is to make a fleet's
coordination legible at a glance — what needs attention, what is claimed, what
is running right now — and to stay keyboard-first, because the humans reviewing
a fleet of agents live in a terminal the rest of the day. It ships no
server-side rendering, no build-time data, and no state the server can't
reconstruct: reload is always a valid recovery.

## Behavior

### Token gate

- WHILE no token is stored in the browser, the app renders only the gate — the
  routed shell never mounts, so no authenticated request is ever issued from an
  unauthenticated session.
- WHEN a token is pasted at the gate, it is checked against `/api/whoami`
  *before* it is persisted; a rejected token yields an inline error and is not
  stored, so a bad token cannot trap the user inside a broken shell.
- WHERE a user has no token, the gate also offers sign-in and registration
  against the public auth endpoints — the only calls made without a bearer;
  both mint a token and enter the shell in the same step.
- WHILE a token is held, it lives client-side (browser storage) and is attached
  as `Authorization: Bearer` on every API call — JSON, raw blob bytes, and SSE
  alike. The browser's native `EventSource` cannot set headers, so streams are
  consumed as an authenticated fetch instead.
- WHEN the user signs out, the token is cleared and the app falls back to the
  gate without a page reload.

### Routing

- WHERE the URL has no repo context, the top bar shows the fleet-wide tabs and
  the routes serve cross-repo aggregates: `/repos`, `/issues`, `/issues/board`,
  `/pulls`, `/pipelines`, `/agents`, plus `/` (fleet overview) and `/settings`.
- WHERE the URL is `/:owner/:repo/…`, the top bar shows the repo tabs and every
  page renders the same breadcrumb card, so repo identity is stated once rather
  than repeated per tab.
- WHERE a static top-level path collides in shape with the dynamic
  `/:owner/:repo` route, the static path is declared first and therefore wins —
  otherwise `/issues/board` would resolve as owner `issues`, repo `board`. The
  shell applies the same rule independently when deriving repo context from the
  path, by treating the reserved first segments as never-a-repo.
- WHEN a route matches nothing, the catch-all renders the styled not-found page
  with a way back, never a blank screen or a raw error body.

### What each tab does

- WHERE the tab is **Code**, it browses the repo tree and blobs at a selectable
  ref, renders the README, and reaches commit history; **Commits** lists history
  grouped by day with incremental paging and per-commit CI status, and a commit
  opens its own diff view.
- WHERE the tab is **Issues**, it is a filterable list (state chips, assignee,
  author, label, search, ready/blocked, sort) with the filters in the URL so a
  view is bookmarkable, plus a **Board** — a drag-to-move column per state that
  writes the new state through the API and refreshes the affected caches.
- WHERE the tab is **Pull requests**, it lists PRs by state, opens a PR's diff,
  conversation, review state and merge action; **Compare** picks base/head from
  the branch list, shows the diff, and opens a PR from it; **Review** is the
  repo-wide inbox of line-anchored code comments, filterable by ref and by
  open/resolved — the same set `cf review` works through.
- WHERE the tab is **Pipelines**, it lists CI runs with state filters and opens
  one run's job DAG with live per-job logs; **Agents** is the same shell scoped
  to agent runs, whose detail view renders the run as a transcript with a
  follow-up message box, and whose runs are spawned from an issue.
- WHERE the tab is **Specs**, it renders the repo's `specs/` tree grouped by
  folder and the selected spec's body, sections (deep-linkable by path and
  section), lifecycle state, drift, editing, and verification.
- WHERE the tab is **Branches**, it lists branches with the default first and
  **Tags** lists tags; **Settings** edits this repo's configuration and members,
  while the account-level `/settings` holds tokens, SSH keys, agent settings and
  the theme selector.

### Data fetching and live updates

- WHILE the app runs, every read goes through one query cache with hierarchical
  keys, so invalidating a prefix (`["issues", owner, repo]`) refreshes every
  derived view — filtered lists, paged lists, and the tab count pills — without
  each call site knowing about the others.
- WHEN a mutation succeeds, it invalidates exactly the key prefixes its write
  can have changed; the UI never hand-patches a cache entry it could refetch.
- IF a request fails with a 4xx, it is not retried — an auth or not-found error
  will not fix itself and retrying only multiplies the request; other failures
  retry a bounded number of times.
- WHILE any run in view is non-terminal, run lists and run detail poll so status
  and duration tick on their own, and polling stops once everything is terminal.
- WHEN a run's job log is opened, the UI subscribes to that job's server-sent
  event stream and reconnects while the run is live, resuming from the last
  sequence it saw; a stream the server closed at a park point is re-opened when
  the run resumes. The fleet overview subscribes to the fleet event feed the
  same way and appends activity live.

### Keyboard navigation and the class contract

- WHILE a list, grid, or file tree is focused, `j`/`k` (and the arrow keys) move
  a roving selection and `Enter` opens it; in a grid `j`/`k` step a whole row
  and `h`/`l` step one cell. Nothing is selected until the first key press.
- WHILE the cursor is in a text field, select, or editable element, the
  navigation keys are not hijacked.
- WHERE a repo route is open, `h`/`l` cycle the repo tabs (Code → Issues →
  Pipelines → Agents) and clamp at the ends rather than wrapping.
- WHERE UI state is visible, it is expressed as an `is-*` class on a stable,
  semantic class name (`is-active`, `is-vim-selected`, `is-loading`), and the
  selected row also carries a `data-vim-selected` attribute. These names are a
  contract, not an implementation detail — the Playwright suite targets them, so
  renaming or hashing them is a breaking change.

### Theme, gallery, and degraded states

- WHERE a color scheme is selected, it is applied by setting a single attribute
  on the document root; all colors resolve through CSS custom properties, so a
  theme is a variable override and nothing else. The base scheme additionally
  follows the OS light/dark preference.
- WHEN the app loads, the stored theme is applied by an inline script before
  first paint, so no frame renders in the wrong scheme. The choice is persisted
  locally and is best-effort: if storage is unavailable the default still
  applies for the session.
- WHERE a base UI primitive exists, it has a row in the `/dev/ui` gallery — the
  living component and design-token reference, checkable against both themes.
- WHILE a query is in flight, the page shows a skeleton of its eventual shape;
  an empty result shows a stated empty state; a failed query shows an inline
  error carrying the server's message, not a stack trace.
- IF an addressed resource (repo, issue, run) returns 404, the page renders the
  not-found view naming what was missing.

## Non-goals

- **Frontend conventions.** File layout, the `@/` alias, the `src/ui` component
  library, semantic BEM, `clsx`, Biome, and the test-running gotchas are the
  contract in `web/CLAUDE.md`. This spec says what the UI does; that doc says
  how the code is written.
- **Server-side behavior of the APIs called here.** Issue claims, PR merges, run
  scheduling, spec parsing, event delivery and token validation are specified by
  their owning specs; this spec only states how the UI surfaces and invalidates
  them.
- **Server-side rendering and hydration.** There is none by design — a static
  bundle served by the daemon, all data fetched client-side.
- **Offline use and optimistic UI.** The cache is a session convenience, not a
  local store; reload from the server is always the recovery path. Nor is
  `src/ui` published as a component library outside this app.
- **The constitution.** Single-binary serving, the local-trust auth posture, and
  pull-based notifications are inherited from `specs/constitution.md`.

## Checklist

- [x] Unauthenticated shell is unreachable; token verified before persisting
- [x] Token held client-side, sent as a bearer on JSON, raw-blob and SSE calls
- [x] Gate offers paste-token / sign-in / register; sign-out returns to it
- [x] Static cross-repo routes declared above the dynamic `/:owner/:repo` route;
      reserved first segments keep aggregates out of repo context
- [x] Repo tabs derive active state from the URL and carry live count pills
- [x] Code/Commits, Issues+Board, Pulls+Compare+Review, Pipelines, Agents,
      Specs, Branches, Tags, Settings each covered by a tab
- [x] One query cache, hierarchical keys, prefix invalidation on mutation; no
      retry on 4xx
- [x] Poll while runs are live; resumable SSE for job logs and the fleet feed
- [x] j/k/Enter list + grid navigation, h/l tab cycling, inert while typing
- [x] `is-*` / `data-vim-selected` state-class contract the Playwright suite targets
- [x] Theme applied pre-paint via a root attribute over CSS custom properties
- [x] `/dev/ui` gallery covers the base primitives in both themes
- [x] Skeleton / empty / inline-error / not-found states on routed pages
- [ ] Verified against the code by the verify workflow (flip to `living`)
