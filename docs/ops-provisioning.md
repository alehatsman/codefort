# Ops provisioning — mooncake → provision (dev loop)

Tracks moongit issue #410. Scope: moongit's local dev-loop tasks move from
mooncake (`tasks.yml`) to provision (`~/projects/futurumlab/provision`), a
new `tasks/` directory of provision plan/component files. The CI runner
(`mgitci.yml`, `cmd/moongitd/ci_runner.go`) and the go-quality/ts-quality
gate stay on mooncake — tracked separately in #411.

## Layout

```
tasks/
  build.yml
  install.yml
  deploy.yml
  backup.yml
  gc.yml
  restore.yml
  uninstall.yml
  create-repo.yml
  run.yml
  ui.yml
  clean.yml
  ci-images.yml
  agent-image.yml
```

Mirrors provision's own repo convention (`tasks/build.yml`, `tasks/ci.yml`,
…): one file per task, `description:` at the top, discovered with
`provision list tasks/`.

No shared `tasks/vars.yml` — see "Shared vars" below for why that didn't
work and what replaced it.

## CLI verb note (version-sensitive, resolved)

The installed `provision` binary moved from 0.9.0 to 0.9.1 mid-implementation
(Provision master mind actively developing it). The two versions disagreed
on the invocation surface for a component/task file:

- **0.9.0:** `apply`/`plan` required a true top-level plan (bare list of
  steps) and rejected a component file outright ("a plan is a list of
  steps, found a mapping"). Component-as-task ran via `run` instead, which
  had no dry-run mode at all.
- **0.9.1:** restored `apply`/`plan`/`list` for component files, matching
  provision's own README (`provision apply tasks/tools.yml`,
  `provision list tasks/`).

Confirmed empirically against the now-installed 0.9.1: `provision apply
tasks/<name>.yml` runs a task, `provision plan tasks/<name>.yml` is a real
dry-run (probes `unless`/`creates` against the machine, prints
ok/changed/would-run, mutates nothing), `provision list tasks/` discovers
them. That's what's used throughout this doc and the invocation table
below.

## Shared vars — why `vars_file` doesn't work here

First attempt: one `tasks/vars.yml` (a plain mapping, direct carry-over of
`tasks.yml`'s `vars:` block) loaded via `vars_file: ./vars.yml` in every
task that needed it — `binary_path: "{{ home }}/.local/bin/moongitd"` etc.

**This is broken by design, not a bug.** Confirmed with Provision master
mind (provision spec §3.3, `ba8bf9a`): a `vars:` step's values are
templates, rendered against the scope when the step executes. A
`vars_file`'s values are **data taken as written** — no render pass at
load, ever, because vars files can legitimately hold shell snippets or
config text where a literal `{{` must survive. Substituting a vars_file
value like `backup_dir` into a step field (`path: "{{ backup_dir }}"`)
inserts its raw text — including the un-rendered `{{ home }}` inside it —
and nothing re-renders that result. Confirmed by real failure: running
`provision apply tasks/backup.yml` created a directory literally named
`{{ home }}` in the invocation cwd. It failed safe (a malformed relative
path, so the real `~/.local/share/moongit` was never touched) but every
task using a home-anchored vars_file value was equally broken.

`~` would work for pure path fields (`file.path/src`, `creates`, `cwd` all
expand it per spec §3), but several of these tasks use the same var as a
`cmd:` argv element (`cmd: [go, build, -o, "{{ binary_path }}", ...]`) —
`cmd` runs without a shell, so there's no tilde expansion there at all,
and no schema-typed "path field" for provision to expand it on its
behalf either. Mixed usage across path-fields and argv in the same files
rules out `~` as a uniform fix.

**Fix:** dropped `tasks/vars.yml` entirely (nothing left in it is
genuinely static — every value was home-anchored). Each task that needs a
home-relative var now has its own inline `- vars:` step, e.g.
(`tasks/install.yml`):

```yaml
steps:
  - vars:
      binary_path: "{{ home }}/.local/bin/moongitd"
      client_path: "{{ home }}/.local/bin/moongit"
  - name: "~/.local/bin directory"
    file: { path: "{{ home }}/.local/bin", state: dir }
  ...
```

This duplicates a handful of lines across 9 files, matches provision's own
working idiom (`tasks/tools.yml`'s `rq_dir: "{{ home }}/.cache/..."` inline
`vars:` step), and is verified correct end to end (see Validation, below).
`--var` on the command line still overrides an inline `vars:` step's value
(§3.3 precedence: CLI > vars/vars_file > facts), so
`mooncake_src`/`dex_src`'s per-box override still works the same way.

## Translations that apply across every task

| mooncake | provision | note |
|---|---|---|
| `file.write: {path, state: directory}` | `file: {path, state: dir}` | rename only |
| `file.write: {path, src, state: link}` | `file: {path, src, state: link}` | identical |
| `shell: {cmd: "..."}` | `shell: \|` (short form) or `shell: {script: "..."}` | field rename in long form |
| `cmd: {argv: [...]}` | `cmd: [...]` | mooncake's `argv:` wrapper drops |
| step-level `cwd:` | `cwd:` | identical, same modifier |
| step-level `env:` | `env:` | identical, same modifier |
| step-level `creates:` | `creates:` | identical, same modifier |
| `log: {msg: "..."}` | `shell: echo "..."` + `changed_when: "false"` | no `log` action in provision (confirmed not on its roadmap) |
| — | `changed_when: "false"` on every ported `shell`/`cmd` step | `validate --strict` requires unless/creates/changed_when on every command step; these are exit-code-is-the-contract steps, same idiom as provision's own `tasks/build.yml` |
| `{{ invocation_dir }}` | `$(pwd)` inside the shell script | provision has no `invocation_dir` template var, but every step's `cwd` defaults to the invocation directory (phase 5b: one rule, no per-file override) unless the step sets its own `cwd:` — none of ci-images/agent-image's steps do, so `$(pwd)` is exactly equivalent |
| shared `vars:` block (`tasks.yml`) | inline `- vars:` step per task | see "Shared vars" above — `vars_file` doesn't render nested `{{ }}`, so home-anchored values can't be shared that way |
| `mooncake apply -c ... -t moongit` invocation model | n/a | out of scope — host/server provisioning lives in dotfiles, untouched |
| a colon+space inside an unquoted `shell: echo "text: text"` value | quote the whole value: `shell: 'echo "text: text"'` | YAML rule (colon-space is invalid in a plain scalar), not a provision quirk — hit twice (install/deploy done-messages) |

## Task-by-task

Ports 1:1 with no structural change; see the translations above for the
mechanical substitutions. Flagging only what's non-obvious per task:

- **build** — no shared vars needed (literal `bin/...` paths).
- **install** — inline `vars:` (`binary_path`, `client_path`).
- **deploy** — inline `vars:` (`binary_path`, `client_path`, `web_dir`).
  The `#300` HEAD-gate guard script, the `npm install`/`creates`/`cwd`
  pair, and the rsync-sync script all port as plain `shell`/`cmd` steps
  unchanged in substance.
- **backup** — inline `vars:` (`backup_dir`, `data_dir`). mktemp/trap/
  sqlite3/tar pipeline is plain shell, ports unchanged.
- **gc** — inline `vars:` (`data_dir`). Repo-walk loop is plain shell.
- **restore** — inline `vars:` (`data_dir`). Takes a caller-supplied
  `tarball` var with no default — invoked as
  `provision apply tasks/restore.yml --var tarball=/path/to/file.tar.gz`
  (mooncake equivalent was `--vars '{tarball: ...}'`). Undefined `tarball`
  is a validate/plan-time error either way (Jinja2 undefined-variable
  rule), matching mooncake's own requirement to pass `--vars`.
- **uninstall** — inline `vars:` (`unit_path`, `binary_path`,
  `client_path`, `data_dir`). Kept as one `rm -f` shell step (matches
  original) rather than three separate `file: {state: absent}` steps —
  `file` takes one path per step, and three tiny steps buys nothing here.
- **create-repo** — inline `vars:` (`binary_path`, `data_dir`). Takes
  caller-supplied `owner`/`name` vars, same undefined-var-is-an-error
  contract as `restore`. `env: {MOONGIT_DATA_DIR: ...}` is a direct
  1:1 (env is a step modifier in both).
- **run** — no shared vars needed. Foreground `go run`, same as today.
- **ui** — no shared vars needed. `npm run dev` never exits — true under
  mooncake too (foreground `cmd`); not run for real in this pass (would
  block indefinitely), schema-validated only.
- **clean** — upgraded from `shell: rm -rf bin` to the typed
  `file: {path: bin, state: absent, force: true}` (force: bin can be a
  non-empty directory) — strictly better idiom, same effect, still a 1:1
  behavioral port.
- **ci-images** — inline `vars:` (`mooncake_src`). Guard check +
  `go -C {{ mooncake_src }} build` + two `docker build` steps, plain
  shell/cmd. Final `log` → `shell: echo`.
- **agent-image** — inline `vars:` (`mooncake_src`, `dex_src`). Three
  guard checks + three cross-repo builds + one `docker build`, plain
  shell/cmd. Final `log` → `shell: echo`.

## Invocation table

| today | after |
|---|---|
| `mooncake task <name>` | `provision apply tasks/<name>.yml` |
| `mooncake task` (list) | `provision list tasks/` |
| `mooncake task restore --vars '{tarball: X}'` | `provision apply tasks/restore.yml --var tarball=X` |
| `mooncake task create-repo --vars '{owner: X, name: Y}'` | `provision apply tasks/create-repo.yml --var owner=X --var name=Y` |
| `mooncake task ci-images --vars '{mooncake_src: X}'` | `provision apply tasks/ci-images.yml --var mooncake_src=X` |

Decided: no Makefile/shim layer to keep the old `mooncake task X` names
typeable — `provision list tasks/` is the discovery command, one more
thing to type is not worth a translation layer (explicit > magic).

## tasks.yml after the split

- The 13 tasks above are deleted.
- The now-dead `vars:` block is deleted (nothing remaining references it).
- Header comment updated: dev-loop tasks live in `tasks/` (provision) now;
  `tasks.yml` (mooncake) holds only the goq/tq-backed gate tasks
  (`tidy`, `fmt`, `test`, `vet`, `lint`, `vuln`, `scan`, `ai-lint*`,
  `arch-snapshot`, `budget-status`, `dupl`, `ci-fast`, `ci`, `ui-*`).

## Validation — done

- `provision validate --strict` clean on all 13 files (`restore`/
  `create-repo` checked with their required `--var` supplied).
- `provision plan` (real dry-run, 0.9.1) clean on every task that wasn't
  applied for real, correct step-by-step preview, no literal `{{ }}`
  anywhere in the output.
- `mooncake task ci` (goq/tq gate) byte-identical result before and after
  the `tasks.yml` edit — same clean build/test/lint, same pre-existing
  `x/crypto` govulncheck failure. The split didn't regress the untouched
  half.
- `tasks.yml` no longer references the moved task names; `mooncake task`
  lists only the retained goq/tq set.
- Applied for real, in blast-radius order:
  - `build`, `clean` — zero risk, local-only. Correct.
  - `backup` — first real run hit the vars_file bug (above) and failed
    safe; re-run after the fix wrote a real 63 MB tarball to
    `~/.local/share/moongit/backups/`. Correct.
  - `install` — rebuilt + reinstalled `moongitd`/`moongit` + aliases.
    Correct.
  - `create-repo` — created `alehatsman/provision-smoke-test` against the
    live data dir. Correct.
  - `ci-images`, `agent-image` — built (docker-cache-hit, no rebuild
    needed since this morning's images were current) `moongit-ci`,
    `moongit-ci-dev`, `moongit-agent`. Correct.
  - `deploy` — **correctly refused**: the #300 upstream guard fired
    because this branch has no upstream (unpushed feature branch), which
    is exactly what it's for. Proves the guard ported faithfully. The
    happy path (actual rebuild + `systemctl restart`) is untested by
    design — it can only run from a checkout level with canonical main,
    which this branch deliberately isn't yet.
- **Not applied for real** (plan-verified only): `restore`, `uninstall` —
  both touch the live moongit host this session's own tooling depends on
  (`restore` rolls the DB back to the backup's timestamp, destroying
  anything written since — including `provision-smoke-test` and session
  issue comments; `uninstall` deletes the systemd unit entirely, and
  recovery is `mooncake apply -c ~/dotfiles/main_pc.yml -t moongit`, a
  different repo, unverified by anything here). Deferred by explicit
  choice, not a gap in the port itself — both showed correct `plan`
  output.
