# Ops provisioning — mooncake → provision

Tracks the mooncake → provision migration (`~/projects/futurumlab/provision`)
in two parts:

- **Dev loop (#410, done).** moongit's local dev-loop tasks moved from
  mooncake (`tasks.yml`) to provision's `tasks/` directory of plan/component
  files. See "Layout" through "Validation — done" below.
- **CI runner (#411, this section).** `mgitci.yml` jobs execute under
  provision instead of mooncake inside `cmd/moongitd/ci_runner.go`. See
  "CI runner" below. The goq/tq quality-gate rewrite itself (native
  provision plans replacing `mooncake task ci`) is explicitly excluded from
  #411 and stays a separate, larger, deferred follow-up — `quality`'s job
  keeps shelling out to `mooncake task ci` as one step inside the
  (now provision-run) job.

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

## CI runner (#411)

Replaces mooncake as `cmd/moongitd/ci_runner.go`'s exec target. This is a
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

### Translation-layer scope (what mgitci.yml actually uses today)

Audited every job in `mgitci.yml`: exactly three step shapes are
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
# mgitci.yml today:
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

`ci/Dockerfile` and `ci/Dockerfile.dev` bake `provision` instead of/
alongside `mooncake`: `mooncake` must stay on PATH as long as the `quality`
job's `mooncake task ci` shell-out is in scope (explicitly excluded from
#411, see top of this section) — audit at implementation time whether both
binaries coexisting in the image is fine (near-certainly yes; disk cost
only) rather than trying to drop mooncake from the image prematurely.

### 7. `host.docker.internal` reachability

`openDockerSession`'s comment (ci_runner.go:856-858) says host reachability
exists so `mooncake task ci` can reach host-served go-quality modules over
http. That's moot for module fetch since #408 (go-quality now resolves via
github). Still needed for: the `smoke` job's `assert` steps (hit
`host.docker.internal:8080` directly) and nothing else identified — keep
the `--add-host` flag, don't remove it.

### Validation plan (not yet run — this section specs the work, doesn't
claim it done)

- A real `mgitci.yml` run — `web` job first (lowest risk, plain `npm`
  steps, no host reachability, no docker_socket) — completes end-to-end
  through the provision-driven container: status, step-level stdout/
  stderr, and timing surface in the moongit UI identically to the mooncake
  path today.
- `quality`'s `mooncake task ci` shell-out still passes inside the
  provision-run container (proves the deferred goq/tq half isn't
  regressed by the image/runner change).
- `smoke`'s translated `assert: {command: curl ...}` steps pass against
  the live host, both the healthy case and (manually, by pointing at a
  wrong port) the failure case, to confirm the curl/grep rewrite actually
  preserves the status+contains semantics.
- A deliberately-failing step in a test job produces the same job-failure
  behavior (stop at first failure, correct exit status, correct UI
  rendering) as today's mooncake path.
- `ci_runner_test.go` passes with the rewritten fakes; `TestParseStepResult`
  is replaced by a streaming-NDJSON-decode equivalent.
