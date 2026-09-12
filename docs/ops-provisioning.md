# Ops provisioning — mooncake → provision

Tracks the mooncake → provision migration (`~/projects/futurumlab/provision`)
in three parts:

- **Dev loop (#410, done).** moongit's local dev-loop tasks moved from
  mooncake (`tasks.yml`) to provision's `tasks/` directory of plan/component
  files. See "Layout" through "Validation — done" below.
- **CI runner (#411, done).** `codefort.yml` jobs execute under provision
  instead of mooncake inside `cmd/codefortd/ci_runner.go`. See "CI runner"
  below. The goq/tq quality-gate rewrite itself was explicitly excluded from
  #411 and deferred — `quality`'s job still shells out to `mooncake task ci`
  (go-quality stays mooncake-only, see below); the `web` job's half of that
  deferral is done, in the next part.
- **Quality gate — ts-quality (this section).** web/'s quality gate moved
  off mooncake's `tasks.yml` (`tq/*`) onto provision's `tasks/ui-*.yml`,
  because upstream ts-quality's own provision migration dropped
  mooncake-module compatibility outright (no more `name:`/`version:` keys —
  provision now *rejects* `name:`). go-quality has not made that jump yet
  (`goq/*` in `tasks.yml` still has `name:`/`version:` and works fine under
  mooncake), so `quality`'s job is untouched — this is ts-quality-specific,
  not a "web gate ports, Go gate doesn't" policy choice.

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
`mooncake_src`'s per-box override still works the same way. (The agent task
also carried a `dex_src` at migration time; the dex integration has since been
removed wholesale, so that var is gone.)

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
  contract as `restore`. `env: {CODEFORT_DATA_DIR: ...}` is a direct
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
  shell/cmd. Final `log` → `shell: echo`. (Since trimmed: the dex build
  input went away with the integration.)

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

## CI runner (#411)

Replaces mooncake as `cmd/codefortd/ci_runner.go`'s exec target. This is a
model change, not a binary swap — see moongit issue #411 for the full
before/after and why. This section is the code gate: no code lands until
this holds.

### Why: two incompatible execution models

**mooncake today:** `internal/ci.MooncakeSteps` (translate.go:107) renders
each job step to a standalone YAML document. `runJob` (ci_runner.go:625)
loops, calling `jobSession.Exec(ctx, stepYAML) (stepResult, error)` once per
step — N subprocess invocations (`mooncake step '<yaml>'`), each returning
one JSON object `{rc,stdout,stderr,duration_ms,changed,failed,skipped,
action,error}`. mooncake never sees a whole job, only one step's YAML at a
time; moongit's event log is synthesized by the Go loop driving it.

**provision:** one whole plan file, one process, streamed NDJSON — one line
per step as it runs, ending in a summary line (confirmed with Provision
master mind, provision spec §8/§9.3; sample in #410's comments):

```
{"event":"step","index":1,"line":3,"name":"say hi","status":"ok","rc":0,"stdout":"hi\n","stderr":"","duration_ms":102}
{"event":"step","index":2,"line":6,"name":"touch nothing","status":"changed","diff":"create directory marker mode 0755","duration_ms":0}
{"event":"step","index":3,"line":8,"name":"skipped","status":"skipped","reason":"when: false","duration_ms":0}
{"event":"step","index":4,"line":11,"name":"fails","status":"failed","rc":4,"stdout":"","stderr":"bad\n","message":"exit","duration_ms":102}
{"event":"summary","plan":"job.yml","total":4,"ok":1,"changed":1,"skipped":1,"failed":1,"unknown":0,"would_change":0,"would_run":0,"would_run_unprobed":0,"interrupted":false,"duration_ms":206}
```

`rc`/`stdout`/`stderr` are present only when the step ran a command
(shell/cmd/assert); absent on typed actions (file/template/pkg/service) and
on skips. Provision's `apply` (no `--keep-going`) stops at the first
failure by default — matches mooncake's current all-or-nothing semantics
(ci_runner.go:680-686), so no behavior change there.

### Translation-layer scope (what codefort.yml actually uses today)

Audited every job in `codefort.yml`: exactly three step shapes are
authored — `run: "<cmd>"` sugar, raw `shell: {cmd: "..."}`, and raw
`assert: {http: {url, status, contains}}` (the `smoke` job). No `cmd:`,
`file:`, `template:`, `pkg:`, or `service:` steps exist in the repo today,
so the translator only needs to handle those three, though a raw
provision-native step (any other top-level key) should still pass through
untouched the way `TranslateJob` does today — same "escape hatch" contract,
just re-targeted.

**Gap: `assert: {http: {...}}` has no provision equivalent.** provision's
`assert` action (spec §6.7) takes `command` (exit-code) or `expr`
(boolean expression over facts/vars) — no built-in HTTP prober. Translated
to a `command`-form assert using `curl -f` (fails non-2xx) piping through
`grep -q` for the body-contains check:

```yaml
# codefort.yml today:
- assert:
    http: { url: "http://host.docker.internal:8080/healthz", status: 200, contains: "ok" }

# translated:
- name: "assert http://host.docker.internal:8080/healthz"
  assert:
    command: 'curl -sf http://host.docker.internal:8080/healthz | grep -q "ok"'
    msg: "http://host.docker.internal:8080/healthz did not return 200 containing \"ok\""
```

`curl -f` alone covers `status: 200` (curl exits nonzero on any 4xx/5xx);
`grep -q` covers `contains`. A future `contains`-less assert (status-only)
drops the pipe. This is a real (if small) behavior-preserving rewrite in
the translator, not a pass-through — documented here because it's the one
construct in current use that provision doesn't express natively.

### 1. `internal/ci` translation shape

Replace `MooncakeSteps(job) ([]MooncakeStep, error)` (a list of
independently-invoked step YAMLs) with:

```go
// TranslateJobPlan renders a job's steps as ONE provision plan document —
// a top-level YAML sequence, provision's plan-file shape (spec §3). run:
// sugar becomes a provision shell step; raw shell/assert steps are
// rewritten per the translations above; any other raw step (a top-level
// key this translator doesn't recognize) passes through untouched, same
// escape hatch TranslateJob offers today. Every emitted shell/cmd/assert
// step gets `changed_when: "false"` (CI steps are exit-code-is-the-
// contract, not idempotent state changes — matches #410's own tasks/*.yml
// idiom) so `validate --strict` (if ever run over these) is clean.
func TranslateJobPlan(job Job) ([]byte, error)
```

`TranslateJob` (mooncake single-file form, still used by... — audit at
implementation time whether anything besides tests still calls it; if not,
retire it rather than keep two translators) and `MooncakeSteps` are
replaced; `inspectStep`/`stepAction`/`shellStep`-equivalent helpers are
reused/adapted, not reinvented — `inspectStep`'s `run:` classification is
shape-agnostic and needn't change.

Each emitted step gets a `name:` derived the same way today's `Label`
is computed (the command for `run:`/`shell:`, a synthesized description for
`assert`) — provision's NDJSON carries `name` per step (see below), and
that's what becomes the event log's `step.started` "name" field, so losing
it would regress the UI's step labels.

### 2. `runJob`'s loop: from per-step `Exec` to one streamed `Exec`

Replace the per-step `sess.Exec(ctx, step.YAML)` calls (ci_runner.go:625-
687) with one call that writes the translated plan to a file in the job's
workspace, runs `provision apply <planfile> --json` once, and reads stdout
line-by-line as NDJSON — architecturally the `streamCommand`/`ExecStream`
machinery already used for agent turns (ci_runner.go:977), not a third
execution model. Concretely:

```go
// jobSession.Exec's replacement: runs ONE provision plan end-to-end,
// invoking onEvent for each NDJSON line as it arrives (so runJob can emit
// step.started/stdout/stderr/completed incrementally, matching today's
// per-step cadence) and returning the parsed summary line once the
// process exits.
type jobSession interface {
    ExecPlan(ctx context.Context, planYAML string, onEvent func(provisionEvent)) (summary, error)
    Close() error
}
```

`dockerSession.Exec` (ci_runner.go:949) becomes `dockerSession.ExecPlan`:
writes `planYAML` to `/work/<job>.plan.yml` inside the container's
bind-mounted workspace, execs `docker exec <name> provision apply
/work/<job>.plan.yml --json`, and streams stdout through `streamCommand`'s
existing `bufio.Reader.ReadBytes('\n')` loop (already handles a line
arriving mid-write — same class of problem, no new machinery). `hostSession`
gets the equivalent non-docker variant. The `docker exec <name> mooncake
step '<yaml>'` invocation (ci_runner.go:950) goes away entirely.

### 3. `stepResult` / `parseStepResult`: from single-shot to streaming

Replace `stepResult` (ci_runner.go:31, one-shot JSON unmarshal of a whole
`mooncake step` invocation) with `provisionEvent`, one per NDJSON line:

```go
// provisionEvent is one line of `provision apply --json` output — either a
// step event or the trailing summary. rc/stdout/stderr/diff/reason/message
// are optional per provision spec §9.3: rc/stdout/stderr only on command
// steps (shell/cmd/assert), diff only when there is one, reason only on a
// skip, message only on a failure.
type provisionEvent struct {
    Event      string `json:"event"` // "step" | "summary"
    Index      int    `json:"index,omitempty"`
    Line       int    `json:"line,omitempty"`
    Name       string `json:"name,omitempty"`
    Status     string `json:"status,omitempty"` // ok | changed | skipped | failed | unknown | would_change | would_run
    RC         *int   `json:"rc,omitempty"`
    Stdout     string `json:"stdout,omitempty"`
    Stderr     string `json:"stderr,omitempty"`
    Diff       string `json:"diff,omitempty"`
    Reason     string `json:"reason,omitempty"`
    Message    string `json:"message,omitempty"`
    DurationMS int    `json:"duration_ms,omitempty"`
    // summary-only fields
    Total, OK, Changed, Skipped, Failed int
}
```

`parseStepResult` (ci_runner.go:1143, single `json.Unmarshal` of a whole
stdout buffer) is replaced by a per-line decode inside the `ExecPlan`
streaming loop — `json.Unmarshal([]byte(line), &ev)` per NDJSON line, not a
whole-buffer parse. An unparseable line is an executor error (same
contract `parseStepResult` has today for unparseable stdout); a cancelled
context is still an executor error regardless of output-so-far.

### 4. Status classification

Today: `failed := res.Failed || res.RC != 0` (ci_runner.go:665), assuming
rc/stdout/stderr are always present — true only because every CI step
today is a command (`run:`/raw `shell:`/raw `assert:`). Provision's typed
actions (file/template/pkg/service — not in use today, but the translator
must not crash if one shows up later) omit rc/stdout/stderr entirely.
Rewritten around provision's `status` string:

```go
failed := ev.Status == "failed"
skipped := ev.Status == "skipped"
// "changed"/"ok" both map to today's non-failed, non-skipped step.completed.
// "would_change"/"would_run"/"unknown" don't occur under `apply` (those are
// plan-only statuses) — apply either does the work or fails; treat their
// appearance as an executor error (a provision version mismatch) rather
// than silently mapping them to something.
```

moongit's job-level status still derives from "any step failed" (not the
summary line's more granular counts) — matches current semantics
(ci_runner.go:664-671), a deliberate no-behavior-change choice, not an
oversight: the summary's `changed`/`ok`/`skipped` breakdown is available
for a future richer job-status view but isn't wired to `storage.JobStatus`
here.

### 5. Test doubles (`ci_runner_test.go`)

Every existing test builds `stepExecutor` as `func(ctx, workDir, stepYAML
string) (stepResult, error)` — one call per step
(`successExec`/`sentinelExec`/`trackingExec`/`blockingExec`). The new shape
is one call per **job**, taking a callback:

```go
type stepExecutor func(ctx context.Context, workDir, planYAML string, onEvent func(provisionEvent)) (summary, error)
```

Each existing fake is rewritten to synthesize a small NDJSON-shaped event
sequence (one `provisionEvent{Status: "ok"}` per step in the plan, or one
`{Status: "failed"}` for `sentinelExec`'s `FAIL_HERE` sentinel) rather than
returning one `stepResult`. `blockingExec` keeps its `<-ctx.Done()` shape —
that's testing the cancellation path, not the per-step contract, and stays
representative either way. No test asserts on `stepResult`'s literal shape
directly (they read back `storage`/`ci.Event` — the stable public
contracts), so the blast radius is the five fakes plus `TestParseStepResult`
(ci_runner_test.go:587), which is replaced by a streaming-decode
equivalent test.

### 6. CI image

Done: `ci/Dockerfile` and `ci/Dockerfile.dev` now bake `provision` alongside
`mooncake` (both binaries coexisting is fine — disk cost only). `mooncake`
stays on PATH as long as the `quality` job's `mooncake task ci` shell-out is
in scope (explicitly excluded from #411, see top of this section).
`ci/README.md` documents producing `ci/provision` (`cargo build --release`,
glibc-linked, fine on `debian:stable-slim`) the same way `ci/mooncake`
already is.

**Also found by actually running it (not anticipated by the spec draft):**
`curl` had to be added to the base image too. mooncake's `assert: {http:
{...}}` used mooncake's own built-in Go HTTP client — no external binary
needed. provision's translated equivalent (the curl-based command assert,
above) does need one, and the base `moongit-ci:latest` image is
deliberately toolchain-free — it didn't carry curl. First live run of the
`smoke` job failed with `curl: command not found`; fixed by adding `curl`
to `ci/Dockerfile`'s package list, documented in `ci/README.md`. This is a
real new image dependency the swap introduces, not a pre-existing gap.

### 7. `host.docker.internal` reachability

`openDockerSession`'s comment (ci_runner.go:856-858) says host reachability
exists so `mooncake task ci` can reach host-served go-quality modules over
http. That's moot for module fetch since #408 (go-quality now resolves via
github). Still needed for: the `smoke` job's `assert` steps (hit
`host.docker.internal:8080` directly) and nothing else identified — keep
the `--add-host` flag, don't remove it.

### Validation — done and not-yet-done

**Done:**

- `go build ./...`, `go vet ./...`, and the full `go test ./...` suite pass
  (every package, not just `internal/ci`/`cmd/codefortd`) — no regression
  anywhere else in the module.
- `internal/ci`'s translator, run for real against the actual `codefort.yml`
  (all three live jobs — `quality`, `web`, `smoke`): `TranslateJobPlan`'s
  output for each job validates clean under the real installed `provision
  0.9.1` binary (`provision validate --strict`), including `smoke`'s
  http-assert rewrite.
- The http-assert curl/grep rewrite, applied for real (`provision apply
  --json`) against the live moongit host at `127.0.0.1:8080` — both the
  healthy case (200 + "ok" body, step reports `ok`) and a deliberately
  wrong-port failure case (step reports `failed`, `msg` surfaced, exit 1) —
  confirming the translation preserves the status+contains semantics, not
  just that it parses.
- The real `--json` NDJSON shape, captured directly from that same live
  `provision apply --json` run, confirmed byte-for-byte against
  `provisionEvent`'s field names/types (including the stdout/stderr
  fd split the spec promises — `--json`'s NDJSON is pure stdout, the
  human-readable summary is pure stderr, verified by redirecting each away
  independently).
- `ci_runner_test.go`'s five fakes (`successExec`/`sentinelExec`/
  `trackingExec`/two `blockingExec` closures) rewritten to the new
  `planExecutor` shape and passing, including the interrupted/timeout/
  cancel discriminator tests; `TestParseStepResult` replaced by
  `TestRunProvisionPlan` (a real subprocess exercising the streaming NDJSON
  decoder — non-zero exit + valid summary is not an error, an unparseable
  line is, a missing summary line is, a cancelled context is).
- Found and fixed one real test-harness bug surfaced by this rewrite (not a
  production bug): `newTestRunner`'s config never set `HostDataDir`, so
  `hostPath`'s identity check silently mismatched and handed the fakes a
  wrong workDir — invisible while no fake touched the filesystem, real once
  `planExecutor` fakes read the plan file `runJob` wrote for real.
- `ci/Dockerfile`, `ci/Dockerfile.dev`, `ci/README.md`, and `ci/.gitignore`
  updated to bake/build `provision` alongside `mooncake`; `codefort.yml`'s
  header comments (the two that stated the mooncake-per-step mechanism as
  current fact, plus one already-stale `mooncake task deploy` reference
  left over from #410) corrected.

**Live end-to-end run — done, against an isolated scratch instance, not the
live moongitd:**

Rebuilt `moongit-ci:latest` from the updated Dockerfile (both binaries +
curl). Built this branch's `moongitd`/`mgit` into a scratch data dir, on a
different port, with a fresh SQLite DB, docker isolation — a separate
process and separate CI-container namespace from the real moongit
deployment, so the live daemon (and its live job queue, if anything had
been running) was never touched. Registered a throwaway repo, enabled CI,
pushed and manually triggered runs through the real `POST .../runs` API
(the push-hook's env-injection path wasn't reached in this pass — a
separate, pre-existing wiring detail unrelated to #411's runner swap; not
chased down since the manual-trigger path exercises the exact same
`executeRun`/`runJob` code either way):

- **Success case** (`smoke` job: a `run:` shell step + an `assert:{http:}`
  step against the scratch server's own `/healthz`): both steps ran through
  the real docker-isolated `provision apply --json` path end to end; job
  and run finished `success`; event log showed correct `step.started` →
  `step.stdout` → `step.completed` for the shell step and correct
  `step.started` → `step.completed` (no stdout/stderr keys — provision
  emitted none, correctly omitted) for the http-assert step.
- **This is what surfaced the curl gap** (§6 above) — the first attempt
  failed with `curl: command not found`; fixed, rebuilt the image, reran,
  passed clean.
- **Failure/skip cascade** (`build`→`test`→`deploy`(`exit 7`)→`notify`):
  `build`/`test` succeeded, `deploy` failed with `exit_code: 7` (the real
  process exit code threaded all the way from the container through
  `provisionEvent.RC` into `storage.CIJob.ExitCode` — not just "some
  failure"), `notify` correctly `skipped` with no exit code. Matches
  today's mooncake-path semantics exactly (same test shape as
  `TestExecuteRunFailurePropagatesAndSkips`, now proven for real, not just
  against a fake).
- Torn down cleanly: scratch process killed, scratch data dir removed, no
  leftover `moongit-ci-*` containers, live moongitd's own `/healthz`
  reconfirmed healthy and untouched throughout.

**One real operational risk found, not yet acted on:** `sweepOrphanContainers`
(ci_runner.go) filters by container name prefix only (`moongit-ci-`/
`moongit-agent-`), not by data dir or port — it's Docker-daemon-wide, not
scoped per moongitd instance. Starting the scratch instance swept 2
pre-existing orphan containers on the shared daemon; harmless this time
(nothing was genuinely in-flight at that moment, confirmed via `docker ps`
before/after), but a second moongitd instance started against the same
Docker daemon while the *live* one has real in-flight CI/agent containers
would force-remove them. Not a regression from #411 (the sweep is
pre-existing, untouched by this change) and out of this issue's scope to
fix, but worth its own issue if a second local instance (staging, another
dev) is ever going to coexist with the production one on one Docker host.

**Still not done:** `quality`'s `mooncake task ci` shell-out inside a
provision-run container (needs `moongit-ci-dev:latest` rebuilt — not done
in this pass, only the base `moongit-ci:latest` was) and a deliberately-
failing step's actual UI rendering (the storage/event-log data it renders
from is proven correct above; the UI component itself wasn't opened).
Neither blocks merging on its own judgment, but flagging both rather than
claiming a clean sweep.

## Quality gate — ts-quality

Replaces `tasks.yml`'s mooncake `tq:` module binding (`ts-quality@v0.1.0`,
`ui-lint`/`ui-build`/`ui-typecheck`/`ui-vuln`/`ui-test`/`ui-ci`/`ui-ci-fast`/
`ui-sync-config`) with six provision task files under `tasks/`
(`ui-tools.yml`, `ui-sync-config.yml`, `ui-config-check.yml`,
`ui-findings.yml`, `ui-fast.yml`, `ui-ci.yml`), and wires `codefort.yml`'s
`web` job to the real gate instead of a bare `npm ci && npm run build`.

### Why now, not deferred further

Upstream `alehatsman/ts-quality` shipped a `feat!: provision migration +
2026 toolchain review` commit (`6cf4279`) that rewrites every component
file from mooncake's shape (`name:`/`version:` keys, `props.fix`-style
templated shell strings) to provision's (`changed_when:`, `timeout:`, typed
`props` with a `description:` per field — and provision *rejects* a
`name:` key outright). No tag past `v0.1.0` exists yet, so the pin below is
to that commit SHA, re-pin to a tag once one lands. The six single-check
mooncake components (`lint.yml`/`format.yml`/`typecheck.yml`/`build.yml`/
`vuln.yml`/`test.yml`) are gone too, folded into `package-scripts.json` npm
scripts — the new module ships only multi-step gates (`ci`/`fast`) plus
config-sync/drift/findings (`sync-config`/`config-check`/`findings`) and
the tools installer (`tools`). Net effect: the old mooncake wiring simply
cannot consume the new module at all, version bump or not.

### Layout

```
tasks/
  ui-tools.yml         checks out ts-quality, npm ci + Playwright browsers
  ui-sync-config.yml   copies biome.base.json + tsconfig.base.json into web/
  ui-config-check.yml  asserts web/ didn't quietly weaken the baseline
  ui-findings.yml      writes web/.gate/findings.jsonl for agents
  ui-fast.yml          pre-commit gate (lockfile, biome staged, typecheck, ai-lint staged)
  ui-ci.yml            full pre-push gate (biome, typecheck, config drift, build, test, supply chain, audit, ai-lint tracked)
```

Mirrors the real precedent already in the fleet for rust-quality
(`isayes`/`teleport`'s `tasks/tools.yml` + `tasks/ci.yml` etc — moongit is
the *first* ts-quality/provision consumer, no prior art in this repo to
copy from directly). The pin lives in `tasks/ui-tools.yml`'s `vars:` step,
nowhere else:

```yaml
- vars:
    tq_ref: 6cf4279755c1bd5b697e94427e3001338252f026
    tq_dir: "{{ home }}/.cache/provision/tools/ts-quality"
- name: "ts-quality at {{ tq_ref }}"
  git:
    repo: https://github.com/alehatsman/ts-quality.git
    dest: "{{ tq_dir }}"
    ref: "{{ tq_ref }}"
```

`ui-tools.yml` does **not** `use:` ts-quality's own `tools.yml` component —
a `use:` target is resolved when the plan is parsed, before any step has
run, so it can't point at a file the git step just cloned in the same plan
(same reason isayes/teleport's `tasks/tools.yml` calls rust-quality's
`scripts/tools.sh` directly instead of `use:`-ing `rq/tools`). Every other
task file (`ui-ci.yml`, `ui-fast.yml`, `ui-sync-config.yml`,
`ui-config-check.yml`, `ui-findings.yml`) is a plain `use:` of the
already-cloned checkout, since by the time those run, `ui-tools.yml` has
already put it on disk.

`web`'s own quality knobs — the `noUnresolvedImports`/`noBaseToString`
overrides, the `tests/**` complexity threshold, `src/ui/index.ts`'s barrel
exemption — live in `web/biome.json`, not in these task files; see
"Findings, and what turned out to be real bugs" below.

### Invocation table

| today | after |
|---|---|
| `mooncake task ui-lint` | *(gone — `cd web && npm run lint`)* |
| `mooncake task ui-build` | *(gone — `cd web && npm run build`)* |
| `mooncake task ui-typecheck` | *(gone — `cd web && npm run typecheck`)* |
| `mooncake task ui-test` | *(gone — `cd web && npm test`)* |
| `mooncake task ui-ci` | `provision apply tasks/ui-tools.yml && provision apply tasks/ui-ci.yml` |
| `mooncake task ui-ci-fast` | `provision apply tasks/ui-fast.yml` |
| `mooncake task ui-sync-config` | `provision apply tasks/ui-sync-config.yml` |
| — | `provision apply tasks/ui-config-check.yml` (new) |
| — | `provision apply tasks/ui-findings.yml` (new) |

The single-check tasks (`ui-lint`, `ui-build`, `ui-typecheck`, `ui-vuln`,
`ui-test`) aren't provision task files at all now — per ts-quality's own
design ("one invocation is an npm script, not a component"), they're
`web/package.json` scripts, run directly from a terminal with no provision
needed. `ui-vuln` specifically folded into `ui-ci`'s supply-chain +
`npm audit` steps; there's no standalone equivalent.

### A real Biome footgun, found the hard way

`biome.json` (the CLI's own config file) is **strict JSON — no `//`
comments** — unlike `tsconfig.base.json`, which Biome happily lints as
JSONC *content*. Adding explanatory `//` comments to `web/biome.json`
silently corrupted config resolution: `biome check .` gave no parse error
at all (only `--config-path biome.json` surfaces the real "Expected a
property" errors), and the effective config quietly fell back to hardcoded
defaults for the fields after the comment — formatter settings reverted to
tabs + forced semicolons while `linter.rules` kept inheriting correctly
from `biome.base.json` (an odd partial-failure split, not a clean "config
ignored"). Cost real time twice in this migration (once diagnosing the
formatter drift, once again when a second comment block was added later
for `files.includes`' rationale). Worth an issue against Biome upstream:
either warn loudly on `//` in `biome.json` specifically, or fail loudly
rather than silently partial-defaulting.

### `extends` does not merge `files.includes`

A second, related footgun: `web/biome.json`'s `extends: ["./biome.base.json"]`
does **not** merge `files.includes` arrays — a child that declares its own
`includes` **replaces** the base's outright. `linter.rules` *does* merge
(confirmed: base's new rules like `noConsole`/`noSecrets` applied
correctly even before this was fixed). Consequence: once `web/biome.json`
needed its own `includes` (to exclude `biome.base.json`/`tsconfig.base.json`
themselves, and the `.grit` plugin source, from being linted as content),
the base's own `!**/node_modules/**`/`!**/dist/**`/etc. excludes were
silently dropped — invisible until a stray `npm run build` left a 900KB
minified `dist/assets/*.js` in the tree, which the gate then tried to lint
(6.5GB RSS, minutes to complete, one false-positive rules-of-hooks
finding from the minified code). Fix: `web/biome.json`'s `files.includes`
repeats the base's six excludes verbatim, then adds moongit-local ones
(`test-results/`, `playwright-report/`, `.vite/`, `*.tsbuildinfo`, `*.log`,
`.playwright-mcp/` — `web/.gitignore`'s entries, since the base's
`vcs.useIgnoreFile: false` means `.gitignore` isn't consulted either).

### Findings, and what turned out to be real bugs

The new baseline (`biome.json` `preset: "recommended"` + the fleet's
targeted additions, `tsconfig.base.json`'s `noUncheckedIndexedAccess` /
`exactOptionalPropertyTypes` / `noPropertyAccessFromIndexSignature` /
`erasableSyntaxOnly`) surfaced ~440 findings against web/'s pre-existing
code (230 typecheck errors, ~210 lint findings before biome's own
`--write` cleared most mechanically). All fixed — gate is green (0 biome
errors/warnings, 0 tsc errors) — full breakdown and file list in the PR;
notable non-mechanical ones:

- **Biome 2.5.13's `noUnresolvedImports`** (nursery) false-positives
  "react has no export named Suspense/Fragment/StrictMode" on every named
  import from react 19 — react's own `.d.ts` plainly exports all three.
  Disabled locally (`web/biome.json`), not fleet-wide — it's a Biome
  version/rule-maturity issue, not a react-19 incompatibility inherent to
  the baseline.
- **`noBaseToString`** (nursery) false-positives on `Date.prototype.
  toLocaleString()` — a real, typed, string-returning method. Same
  disposition: disabled locally, flagged as nursery-rule noise.
- **A real, pre-existing bug found by `exactOptionalPropertyTypes`:**
  `useCommitCIStatus` and `useCIRuns` (`web/src/api/queries.ts`) shared one
  `refetchInterval` callback (`ciRunsRefetchInterval`) typed for
  `data: CIRun[]`, even though `useCommitCIStatus`'s own `select` maps that
  array to a `Map<string, CIRun>`. TypeScript's inference, anchored by the
  shared callback's explicit param type, silently widened
  `useCommitCIStatus`'s resolved type back to `CIRun[]` — masking real
  `.get()`-does-not-exist-on-`CIRun[]` errors at every call site
  (`RepoPage.tsx`, `PullPage.tsx`) until this pass's strict typecheck
  actually ran clean. Fixed by giving each query its own typed
  `refetchInterval`, sharing only the `anyRunLive()` predicate.
- **`noExcessiveCognitiveComplexity`** (max 15) flagged 30 functions
  (up to score 78) — mostly SSE reconnect-loop hooks (`ciEvents.ts`,
  `useFleetEvents.ts`) and large page components. Genuinely refactored
  (module-scope extraction of nested closures, switch-per-case dispatch
  helpers, JSX sub-component extraction) rather than raising the
  threshold — the one exception is `tests/mockApi.ts` (a mock HTTP router;
  threshold raised to 20 for `tests/**` only, in `web/biome.json`'s
  `overrides`), where the complexity is inherent to being a router, not a
  smell.

### Validation — done

- `provision validate --strict` clean on all six `tasks/ui-*.yml` files.
- `provision apply tasks/ui-tools.yml` run for real: clones ts-quality to
  `~/.cache/provision/tools/ts-quality`, `npm ci` + Playwright browsers.
- `provision apply tasks/ui-sync-config.yml` run for real: wrote
  `web/biome.base.json` (updated) and `web/tsconfig.base.json` (new).
- `provision apply tasks/ui-ci.yml` (the full gate) run for real, clean:
  `npx biome check --error-on-warnings --max-diagnostics=none .` → 0
  errors, 0 warnings (99 `info`-level `useLiteralKeys` suggestions, which
  don't fail the gate); `npx tsc -b` → 0 errors; `npm run build` → succeeds
  (Vite production build); `npx playwright test` → 126 passed, 18 failed —
  **the same 18**, byte-for-byte matching test names, confirmed by running
  the identical suite against unmodified `main` via `git stash`. Zero
  regressions from this migration; the 18 are pre-existing flakiness/gaps
  unrelated to it.
- `tasks.yml`'s mooncake `tq:` module binding and all `ui-*` task entries
  removed; `mooncake task` (Go gate only) still lists cleanly.
- `codefort.yml`'s `web` job updated to `provision apply tasks/ui-tools.yml`
  then `provision apply tasks/ui-ci.yml` — **not yet run for real in CI**
  (would require a push through the live pipeline); the component-level
  validation above exercises the identical steps the job now runs, just
  not inside the `moongit-ci-dev:latest` container via `ci_runner.go`.
  Flagging as the one piece not end-to-end proven, matching this doc's own
  standard elsewhere (the CI-runner section flags its own not-yet-done
  items rather than claiming a clean sweep).

