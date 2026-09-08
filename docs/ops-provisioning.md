# Ops provisioning — mooncake → provision (dev loop)

Tracks moongit issue #410. Scope: moongit's local dev-loop tasks move from
mooncake (`tasks.yml`) to provision (`~/projects/futurumlab/provision`), a
new `tasks/` directory of provision plan/component files. The CI runner
(`mgitci.yml`, `cmd/moongitd/ci_runner.go`) and the go-quality/ts-quality
gate stay on mooncake — tracked separately in #411.

## Layout

```
tasks/
  vars.yml         # shared vars, loaded via vars_file by every task below
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
`provision run tasks/` (trailing slash lists a directory — the installed
CLI, v0.9.0, uses `run` for a component-as-task; `apply`/`plan` are
reserved for a true top-level plan file — a bare list of steps — and error
on a component file with "a plan is a list of steps, found a mapping".
provision's own README shows `apply` for this; that's ahead of what's
installed here. Confirmed empirically, not assumed).

## Shared vars (`tasks/vars.yml`)

Direct carry-over of `tasks.yml`'s `vars:` block — a plain mapping, loaded
via `vars_file: ./vars.yml` in every task that needs it:

```yaml
binary_path: "{{ home }}/.local/bin/moongitd"
client_path: "{{ home }}/.local/bin/moongit"
data_dir:    "{{ home }}/.local/share/moongit"
unit_path:   "{{ home }}/.config/systemd/user/moongit.service"
backup_dir:  "{{ home }}/.local/share/moongit/backups"
web_dir:     "{{ home }}/projects/moongit/web/dist"
mooncake_src: "{{ home }}/projects/mooncake"
dex_src:      "{{ home }}/projects/dex"
```

`{{ home }}` is a provision **fact** (§5), not a var — resolves the same as
mooncake's `home`, no definition needed.

Once these 13 tasks move out, **nothing left in `tasks.yml` references this
vars block** (the goq/tq-backed tasks don't touch it) — so `tasks.yml`'s
`vars:` block is deleted, not just left dangling.

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
| `mooncake apply -c ... -t moongit` invocation model | n/a | out of scope — host/server provisioning lives in dotfiles, untouched |

## Task-by-task

Ports 1:1 with no structural change; see the translations above for the
mechanical substitutions. Flagging only what's non-obvious per task:

- **build** — no vars_file needed (literal `bin/...` paths, no shared var).
- **install** — needs `vars_file` (`binary_path`, `client_path`). Final
  `log` step → `shell: echo "..."`.
- **deploy** — needs `vars_file` (`binary_path`, `client_path`, `web_dir`).
  The `#300` HEAD-gate guard script, the `npm install`/`creates`/`cwd`
  pair, and the rsync-sync script all port as plain `shell`/`cmd` steps
  unchanged in substance. Long-running risk: none, this step set still
  runs to completion and exits (unlike `ui`, below).
- **backup** — needs `vars_file` (`backup_dir`, `data_dir`). mktemp/trap/
  sqlite3/tar pipeline is plain shell, ports unchanged.
- **gc** — needs `vars_file` (`data_dir`). Repo-walk loop is plain shell.
- **restore** — needs `vars_file` (`data_dir`). Takes a caller-supplied
  `tarball` var with no default — invoked as
  `provision run tasks/restore.yml --var tarball=/path/to/file.tar.gz`
  (mooncake equivalent was `--vars '{tarball: ...}'`). Undefined `tarball`
  is a plan-time error either way (Jinja2 undefined-variable rule),
  matching mooncake's own requirement to pass `--vars`.
- **uninstall** — needs `vars_file` (`unit_path`, `binary_path`,
  `client_path`). Kept as one `rm -f` shell step (matches original) rather
  than three separate `file: {state: absent}` steps — `file` takes one
  path per step, and three tiny steps buys nothing here.
- **create-repo** — needs `vars_file` (`binary_path`, `data_dir`). Takes
  caller-supplied `owner`/`name` vars, same undefined-var-is-an-error
  contract as `restore`. `env: {MOONGIT_DATA_DIR: ...}` is a direct
  1:1 (env is a step modifier in both).
- **run** — no vars_file needed. Foreground `go run`, same as today.
- **ui** — no vars_file needed. `npm run dev` never exits — this was true
  under mooncake too (foreground `cmd`); provision streams the same way
  under a TTY per spec §9.1. Flagged as a validation item below, not a
  known gap.
- **clean** — upgraded from `shell: rm -rf bin` to the typed
  `file: {path: bin, state: absent, force: true}` (force: bin can be a
  non-empty directory) — strictly better idiom, same effect, still a 1:1
  behavioral port.
- **ci-images** — needs `vars_file` (`mooncake_src`). Guard check +
  `go -C {{ mooncake_src }} build` + two `docker build` steps, plain
  shell/cmd. Final `log` → `shell: echo`.
- **agent-image** — needs `vars_file` (`mooncake_src`, `dex_src`). Three
  guard checks + three cross-repo builds + one `docker build`, plain
  shell/cmd. Final `log` → `shell: echo`.

## Invocation table

| today | after |
|---|---|
| `mooncake task <name>` | `provision run tasks/<name>.yml` |
| `mooncake task` (list) | `provision run tasks/` |
| `mooncake task restore --vars '{tarball: X}'` | `provision run tasks/restore.yml --var tarball=X` |
| `mooncake task create-repo --vars '{owner: X, name: Y}'` | `provision run tasks/create-repo.yml --var owner=X --var name=Y` |
| `mooncake task ci-images --vars '{mooncake_src: X}'` | `provision run tasks/ci-images.yml --var mooncake_src=X` |

Decided: no Makefile/shim layer to keep the old `mooncake task X` names
typeable — `provision run tasks/` is the discovery command, one more
thing to type is not worth a translation layer (explicit > magic).

## tasks.yml after the split

- The 13 tasks above are deleted.
- The now-dead `vars:` block is deleted (nothing remaining references it).
- Header comment updated: dev-loop tasks live in `tasks/` (provision) now;
  `tasks.yml` (mooncake) holds only the goq/tq-backed gate tasks
  (`tidy`, `fmt`, `test`, `vet`, `lint`, `vuln`, `scan`, `ai-lint*`,
  `arch-snapshot`, `budget-status`, `dupl`, `ci-fast`, `ci`, `ui-*`).

## Validation plan

- `provision validate --strict` on every new `tasks/*.yml` file (the two
  that take caller-supplied vars — `restore`, `create-repo` — validated
  with `--var` supplied, since an undefined template var is a validate-time
  error, not just an apply-time one).
- **No dry-run tier exists for component/task files** with the installed
  CLI: `provision plan`/`provision apply` both require a true top-level
  plan (a bare list of steps) and reject a component file outright
  ("a plan is a list of steps, found a mapping") — confirmed empirically,
  not assumed from the README (which shows `apply` for this; the installed
  binary is behind it). `run`'s own flag set has no `--dry-run`/`--plan`
  equivalent. So a task beyond `validate --strict` can only be checked by
  actually running it — there is no read-only middle tier here.
- Real `provision run` runs, in order of blast radius (ask before
  anything past the first tier):
  1. **Zero-risk, local-only:** `build`, `clean` — safe to actually run.
  2. **Touches installed/live state:** `install`, `deploy`, `backup`, `gc`,
     `restore`, `uninstall`, `create-repo`, `ci-images`, `agent-image` —
     schema-validated only in this pass; a real run of any of these
     touches the running moongit service or its data and needs an explicit
     go-ahead first, per the shared-prod risk `deploy` itself already
     guards against (#300).
- `mooncake task ci` (goq/tq gate) still passes unchanged after
  `tasks.yml`'s edit — proves the split didn't regress the untouched half.
- `tasks.yml` no longer references the moved task names; `provision run
  tasks/` shows them instead.
