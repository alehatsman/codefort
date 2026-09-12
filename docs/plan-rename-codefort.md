# Plan of work — moongit → codefort

Total rename and rebrand. `moongit` the product becomes **codefort**; the
daemon `moongitd` becomes `codefortd`; the client `moongit`/`mgit` becomes
**`cf`**. Nothing about behavior changes — this plan is a pure rename, and any
line of it that starts wanting to also fix something is out of scope.

Status legend: `todo` / `doing` / `done` / `dropped`.

## Decisions (locked)

| Question | Answer | Consequence |
|---|---|---|
| Binaries | `codefortd` (daemon), `cf` (client) | `cmd/moongitd` → `cmd/codefortd`, `cmd/moongit` → `cmd/cf` |
| CI manifest | `codefort.yml`, **hard cut** | `mgitci.yml` is no longer read. Every hosted repo must rename its file at cutover or its CI silently stops firing. |
| Env vars | `CODEFORT_*`, **hard cut** | 33 vars. A stale `MOONGIT_X` is not an error — the server starts with that setting's *default*. Silent, not loud. See "Traps". |
| Token prefix | `mgt_` → `cf_` | Cosmetic only. Tokens are stored hashed and nothing validates the prefix on read (`internal/storage/tokens.go:18` is the sole definition, used only at generation), so **every existing `mgt_` token keeps working**. |
| Data paths | unchanged | `moongit.db` and the data dir keep their names. Renaming them means moving live data for no functional gain; an operator who wants it renames the file and sets `CODEFORT_DB_PATH`. This is the one deliberate leftover. |

## Inventory

Measured on `main` at plan time, excluding `node_modules` and `.git`:

| Token | Hits | Becomes |
|---|---|---|
| `moongit` | 807 | `codefort` |
| `MOONGIT_` | 383 | `CODEFORT_` |
| `mgit` | 217 | `cf` (or `codefort.yml` / `codefortd` — see Traps) |
| `moongitd` | 164 | `codefortd` |
| `mgitci` | 63 | `codefort.yml` |
| `mgt_` | 40 | `cf_` |
| `mgitd` | 7 | `codefortd` |

Surface: 102 Go files import `github.com/alehatsman/moongit`; 28 Markdown files
mention the name; 15 web source files carry branding; 19 provision plans in
`tasks/`; 3 Dockerfiles; 2 named directories under `cmd/`; 1 named file at the
repo root (`mgitci.yml`).

## Traps

These are the parts that a `sed -i s/moongit/codefort/g` gets wrong. They are
the reason this is a plan and not a one-liner.

1. **`mgit` is a substring of `mgitci` and `mgitd`.** Replacement must run
   longest-token-first: `mgitci` → `codefort.yml`, then `moongitd` →
   `codefortd`, then `mgitd` → `codefortd`, then `moongit` → `codefort`, then
   `MOONGIT_` → `CODEFORT_`, then `mgt_` → `cf_`, and **`mgit` → `cf` last and
   by hand**.
2. **`cf` is a two-letter token and must never be a sed target.** `mgit` →
   `cf` gets applied with a word boundary and every hunk read before it lands.
   The reverse direction (anything → `cf`) is where a careless rename corrupts
   unrelated words.
3. **The env hard cut fails silently.** `envOr("MOONGIT_DATA_DIR", "data")`
   becoming `envOr("CODEFORT_DATA_DIR", "data")` means a systemd unit still
   exporting `MOONGIT_DATA_DIR` yields a server running against `./data` —
   wrong repos, wrong DB, no error. The cutover runbook below updates the unit
   *before* the binary. Mitigation worth building: `codefortd` logs a loud
   warning at startup for any `MOONGIT_*` var still present in the
   environment. That is ~10 lines and turns the silent failure into an audible
   one. **Recommended; it is the only new code in this plan.**
4. **The CI manifest hard cut is a flag day for every hosted repo.** The
   running daemon reads `mgitci.yml`; the new one reads `codefort.yml`. Between
   deploy and each repo renaming its file, that repo has no CI. This repo's own
   manifest is renamed in Phase 3 — which means CI on *this* repo stops the
   moment Phase 3 lands and resumes only once `codefortd` is deployed.
   Sequencing in the runbook.
5. **Merge-commit authorship is persisted.** `internal/server/merge.go:547`
   names the committer `moongit` on server-side merges. Renaming it changes
   only future commits; history keeps the old name, correctly.
6. **Web localStorage keys are user-visible state.** `moongit_token`
   (`web/src/api/client.ts:58`), `moongit:theme` (`web/src/theme.ts:19`), and
   `moongit:repo-order` (`web/src/features/repo/useRepoOrder.ts:4`). Hard-cutting
   the token key logs every browser session out. Judgment call made here: the
   token key gets a one-shot read of the old key on first load (five lines,
   deleted a release later); theme and repo-order hard-cut, because re-picking
   a theme is not a support incident.
7. **Agent handoff writes a git remote named `moongit`**
   (`cmd/codefortd/agent_handoff.go:285`). Parked agent runs with existing
   worktrees keep the old remote name; the code adds-or-sets-url by name, so a
   renamed remote leaves a stale `moongit` remote behind in those worktrees.
   Harmless, but it is why parked runs should be drained before cutover.
8. **`moongit.service` and the dotfiles deploy gate live outside this repo.**
   `tasks/deploy.yml` references `moongit.service` and a HEAD-gated
   `mooncake apply -t moongit` in dotfiles. The service rename is an external
   change that must land in the same window.

## Phases

Each phase is one commit and each leaves the tree building. Phase 1 is the only
one that must be atomic internally.

### Phase 1 — Go module path and package directories  `todo`

The atomic one: the module path, the 102 importing files, and the two command
directories move together or nothing compiles.

- `go mod edit -module github.com/alehatsman/codefort`
- Rewrite the import path across all Go files. The string
  `github.com/alehatsman/moongit` is unique and unambiguous — this is the one
  safe bulk substitution in the plan.
- `git mv cmd/moongitd cmd/codefortd` and `git mv cmd/moongit cmd/cf`.
- Package clause and internal references inside those two dirs.
- Gate: `gofmt -l`, `go build ./...`, `go vet ./...`, `go test ./...`.

Branch/commit: `refactor(rename): module path and command dirs → codefort`.

### Phase 2 — environment variables  `todo`

- `internal/config/config.go` + `config_test.go` (190 hits between them) —
  every `envOr("MOONGIT_…")` key.
- The startup warning from Trap 3: scan `os.Environ()` for a `MOONGIT_` prefix
  and log each one found at WARN with "ignored, renamed to CODEFORT_…".
- Propagate to `tasks/*.yml`, the three Dockerfiles, `agent/README.md`,
  `ci/README.md`, `docs/config.md`.
- Agent container env (`cmd/codefortd/agent_creds.go`) hands `CODEFORT_TOKEN` /
  `CODEFORT_SERVER` to the container; the agent image's expectations move with
  it.

### Phase 3 — CI manifest  `todo`

- Every reader and every doc: `mgitci.yml` → `codefort.yml`
  (`cmd/codefortd/ci_runner.go`, `cron_scheduler.go`, `cmd/cf/main.go`).
- `git mv mgitci.yml codefort.yml` — this repo's own manifest, with its
  branch-prefix gate intact.
- No fallback path is added. That was the decision; Trap 4 is its cost.

### Phase 4 — images, services, provision tasks  `todo`

- `moongit-ci:latest` → `codefort-ci:latest`, `moongit-agent:latest` →
  `codefort-agent:latest` (config defaults + `tasks/ci-images.yml` +
  `tasks/agent-image.yml` + the Dockerfiles).
- `tasks/` (19 plans): binary paths `~/.local/bin/codefortd` and
  `~/.local/bin/cf`, the `moongit.service` → `codefort.service` references in
  `deploy.yml`, and the install/uninstall symlink names.
- `tasks.yml` descriptions.

### Phase 5 — web UI  `todo`

15 files. Visible title and shell branding (`web/index.html`,
`web/shell/Layout.tsx`, `features/settings/TokenGate.tsx` — the last one tells
the user to run `moongitd token create`, which becomes `codefortd token
create`), `package.json` name, the API types comment, and the three
localStorage keys per Trap 6. Playwright specs that assert on branding move with
it.

### Phase 6 — docs, specs, and agent instructions  `todo`

28 Markdown files. `README.md`, `VISION.md`, `ROADMAP.md`, `CLAUDE.md`,
`AGENTS.md`, `web/CLAUDE.md`, all six `docs/`, all fifteen `specs/`.

Spec frontmatter `covers:` lists file paths — `cmd/moongit/main.go` entries
become `cmd/cf/main.go`, or the deterministic drift backstop starts reporting
every spec as covering a missing file. This is the phase most likely to be
skimped and the one with a machine that will catch it.

`docs/ops-provisioning.md` and `docs/plan-2026-09-review.md` are historical
records; they get renamed too, since they describe the same live system.

### Phase 7 — token prefix  `todo`

`internal/storage/tokens.go:18` → `cf_`, plus the test fixtures and the
`export MOONGIT_TOKEN=mgt_…` line in the client usage text. Isolated on purpose:
it is the one change that touches credential-shaped strings, and it is easier to
reason about alone than buried in a 400-file diff.

### Phase 8 — repository rename  `todo`

- GitHub: `alehatsman/moongit` → `alehatsman/codefort`. GitHub redirects the
  old URL, so `origin` keeps working, but `git remote set-url` anyway.
- The self-hosted copy: this repo is hosted on its own server, so the rename is
  also a codefort-side repo rename, and the `moongit` git remote in every local
  clone and worktree becomes `codefort`.
- The agent handoff remote name (Trap 7) and the MCP server name
  (`cmd/cf/mcp.go:153`) move here.

## Cutover runbook

Order matters, because Phases 2 and 3 are both hard cuts against a live box.

1. Drain: no running CI or agent runs; park nothing new.
2. Back up: `provision apply tasks/backup.yml`.
3. Merge Phases 1–7 to `main`. **CI on this repo is now dark** — the deployed
   daemon is still looking for `mgitci.yml`.
4. Update the systemd unit (external, dotfiles): rename to `codefort.service`,
   rewrite every `MOONGIT_*` export to `CODEFORT_*`, point `ExecStart` at
   `codefortd`. Do not start it yet.
5. Rebuild the container images under their new names
   (`tasks/ci-images.yml`, `tasks/agent-image.yml`) — the new daemon's defaults
   name images that do not exist yet.
6. `provision apply tasks/deploy.yml` (itself renamed), then start
   `codefort.service`.
7. Verify: web UI loads and is branded; `cf issue list` against the server;
   push a commit to this repo and confirm CI fires on `codefort.yml`; confirm an
   existing `mgt_` token still authenticates.
8. Rename each *other* hosted repo's `mgitci.yml` → `codefort.yml` and push.
   Until a repo does this it has no CI, with no error to tell you.
9. Phase 8: rename on GitHub, re-point remotes.

Rollback is the backup from step 2 plus reinstalling the previous binaries; the
DB is untouched by every phase, which is what makes rollback cheap.

## Deliberately not in scope

- **Any behavior change.** If a rename exposes a bug, it gets its own issue and
  its own commit, not a ride-along.
- **`moongit.db` and the data directory.** Per the decisions table — live data
  stays put.
- **A compatibility shim of any kind.** No `mgit` symlink, no `MOONGIT_*`
  fallback read, no `mgitci.yml` fallback. Hard cut was chosen deliberately; the
  startup warning in Phase 2 is diagnostics, not compatibility.
- **The mooncake → provision migration.** Still blocked upstream on
  `alehatsman/go-quality`; the rename touches its call sites but does not
  advance it.
