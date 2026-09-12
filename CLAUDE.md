# codefort

Self-hosted git host, issue tracker, and CI runner: one Go daemon
(`codefortd`) serving `/api/*`, git smart-HTTP, and the Vite/React SPA on a
single port, over one SQLite file and a directory of bare repos. `cf` is
the client. This repo is the coordination backend itself — dogfood it.

## Workflow — track work as codefort issues (cf)

Prereq: the repo has a `codefort` remote (code mirror) — or `CODEFORT_SERVER`
points at the server. Export your **own** `CODEFORT_TOKEN` (`cf_…`); the
token's name is your identity in every claim/comment, so never share one.

1. **Survey:** `cf issue list --state todo,in_progress`.
2. **Plan as issues** — one issue per unit of work, the plan in the body. Split
   multi-part work into multiple issues:
   `cf issue create --title "<t>" --body "<plan>"`.
3. **Claim before coding:** `cf issue claim <n> --state in_progress`. Never
   work an issue already `in_progress` under another identity.
4. **Report progress** at real checkpoints: `cf issue comment <n> --body "…"`.
5. **Close out** when merged + verified: `cf issue set-state <n> done`
   (`cf issue unclaim <n>` if you drop it).

No code without an owned issue.

## Commands

Go — the everyday loop, no tooling required:

```bash
go build ./...          # compile everything
go test ./...           # Go tests
gofmt -w .              # format (same as the `fmt` task)
go run ./cmd/codefortd   # run the server against ./data/
```

**Go quality gate:** `mooncake task ci` — the full lint/vuln/scan/ai-lint/
arch/dupl gate, from the shared `go-quality` module pinned in `tasks.yml`.
`mooncake task` lists the rest (`test`, `vet`, `lint`, `vuln`, `ci-fast`,
`tidy`, …). This is the **last remaining mooncake tie**: mooncake is being
replaced by provision and everything else has already moved — see
`docs/ops-provisioning.md`.

**Web gate:** from the repo root,

```bash
provision apply tasks/ui-tools.yml   # clone ts-quality + install web/ deps (first)
provision apply tasks/ui-ci.yml      # full gate: biome, typecheck, build, test, audit
```

Or directly from `web/` for the fast inner loop: `npm run lint`,
`npm run lint:fix`, `npm run typecheck`, `npm test`, `npm run dev`.

**Dev loop:** the build/install/deploy/run/backup/gc/restore tasks are
provision plans in `tasks/`.

```bash
provision list tasks/                # discover every task + its description
provision apply tasks/build.yml      # both binaries into ./bin/
provision apply tasks/install.yml    # binaries into ~/.local/bin + cf/codefortd links
provision apply tasks/run.yml        # codefortd in the foreground against ./data/
```

**CI** (`codefort.yml`) runs two independent jobs in throwaway containers:
`quality` (`mooncake task ci`) and `web` (the two `ui-*` plans above). Deploy
is deliberately not automated — `provision apply tasks/deploy.yml`.

## Docs map

| Read | When |
| --- | --- |
| `VISION.md` | Before proposing anything structural — the three commitments and the explicit nos. |
| `ROADMAP.md` | What's shipped, what's next, what was rejected. |
| `docs/architecture.md` | How the one process fits together; start here on any backend change. |
| `docs/api.md` | The JSON API reference (route table lives in `internal/server/server.go`). |
| `docs/config.md` | The full `CODEFORT_*` env reference — there is no config file. |
| `docs/specs.md` | The spec format contract: front-matter, layout, what the parser guarantees. |
| `docs/ops-provisioning.md` | The mooncake → provision migration; task/command equivalences. |
| `specs/` | Per-subsystem specs. `specs/constitution.md` is the repo-wide contract every other spec inherits — read it first. |
| `web/CLAUDE.md` | **Read before touching `web/`** — feature layout, `@/` imports, BEM, `clsx`, Biome, Playwright. |
| `agent/README.md` | The agent base image and how agent runs are executed. |
| `ci/README.md` | The CI base image and what every CI image must carry. |

## House rules

- **Spec-first** for non-trivial work: update the governing spec in `specs/`
  before the code. Spec and code disagree → surface it and ask; never let them
  silently diverge.
- **Stay in scope.** No unrequested refactors, dependency additions, or
  modernization. New direct deps are a cost, not a convenience (`VISION.md`).
- **Worktrees** for non-trivial changes — outside the repo, one per branch.
- **Conventional branches and commits.** Prefixes CI accepts are listed in
  `codefort.yml`; an unlisted prefix silently loses pre-merge CI.
- **Never auto-push `main`.** Ask before merge.
- **Cite `path:line`** when reporting findings. Investigation is read-only.
