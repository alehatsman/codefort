import clsx from "clsx"
import { Fragment, useMemo, useState } from "react"
import { Link, useNavigate, useParams } from "react-router-dom"
import { useCIRun, useCIRuns, useRefs, useRepo } from "../api/queries"
import { useRerunCIRun, useSetCIEnabled, useTriggerCIRun } from "../api/mutations"
import { useJobEventStream } from "../lib/ciEvents"
import { absoluteTime, timeAgo } from "../lib/timeAgo"
import type { CIEvent, CIJob, CIRun, Repo } from "../api/types"
import RepoHeader from "../components/RepoHeader"
import CIStatusBadge from "../components/CIStatusBadge"

// Pipelines tab. One component serves the runs list (/pipelines) and a single
// run's detail (/pipelines/:number), distinguished by the URL — mirroring how
// RepoPage serves the code browser routes.
export default function PipelinesPage() {
  const { owner = "", repo = "" } = useParams()
  const numberParam = useParams().number
  const repoQ = useRepo(owner, repo)

  if (repoQ.isLoading) return <div className="loading">Loading…</div>
  if (repoQ.error) return <div className="error">{(repoQ.error as Error).message}</div>
  if (!repoQ.data) return null

  const r = repoQ.data
  const runNumber = numberParam ? Number(numberParam) : null

  return (
    <div className="repo">
      <RepoHeader owner={r.owner} repo={r.name} openIssues={r.open_issues} />
      {runNumber !== null && Number.isFinite(runNumber) ? (
        <RunDetail owner={r.owner} repo={r.name} runNumber={runNumber} />
      ) : (
        <RunList repo={r} />
      )}
    </div>
  )
}

function RunList({ repo }: { repo: Repo }) {
  const { owner, name } = repo
  if (!repo.ci_enabled) return <CIDisabledCard owner={owner} repo={name} />

  return <EnabledRunList owner={owner} repo={name} />
}

function EnabledRunList({ owner, repo }: { owner: string; repo: string }) {
  const runsQ = useCIRuns(owner, repo)
  const setEnabled = useSetCIEnabled(owner, repo)
  const refsQ = useRefs(owner, repo)
  const trigger = useTriggerCIRun(owner, repo)
  // The input defaults to the repo's default branch until the user edits it
  // (null = untouched, so a freshly loaded default still flows through).
  const [refInput, setRefInput] = useState<string | null>(null)
  const ref = refInput ?? refsQ.data?.default ?? ""

  function runPipeline(e: React.FormEvent) {
    e.preventDefault()
    const r = ref.trim()
    if (!r) return
    trigger.mutate(r)
  }

  return (
    <section className="pipelines">
      <div className="pipelines__head">
        <h2 className="pipelines__title">Pipelines</h2>
        <div className="pipelines__actions">
          <form className="pipelines__run" onSubmit={runPipeline}>
            <input
              className="input pipelines__run-ref"
              value={ref}
              onChange={(e) => setRefInput(e.target.value)}
              placeholder="branch, tag, or commit"
              aria-label="Ref to run"
            />
            <button
              type="submit"
              className="btn btn--small btn--primary"
              disabled={trigger.isPending || ref.trim() === ""}
            >
              {trigger.isPending ? "Running…" : "Run pipeline"}
            </button>
          </form>
          <button
            type="button"
            className="btn btn--small"
            onClick={() => setEnabled.mutate(false)}
            disabled={setEnabled.isPending}
            title="Disable CI for this repo"
          >
            Disable CI
          </button>
        </div>
      </div>
      <p className="muted small pipelines__lead">
        Runs trigger on push when an <code>mgitci.yml</code> is present at the pushed commit, or on
        demand for any ref above.
      </p>
      {trigger.error && <div className="error inline">{(trigger.error as Error).message}</div>}

      {runsQ.isLoading && <div className="loading">Loading…</div>}
      {runsQ.error && <div className="error">{(runsQ.error as Error).message}</div>}
      {runsQ.data && runsQ.data.length === 0 && (
        <div className="empty">
          No runs yet. Push a commit with an <code>mgitci.yml</code> to trigger the first one.
        </div>
      )}

      {runsQ.data && runsQ.data.length > 0 && (
        <table className="ci-runs">
          <thead>
            <tr>
              <th>Run</th>
              <th>Status</th>
              <th>Commit</th>
              <th>Ref</th>
              <th>Trigger</th>
              <th>Duration</th>
              <th>When</th>
            </tr>
          </thead>
          <tbody>
            {runsQ.data.map((run) => (
              <tr key={run.number}>
                <td>
                  <Link className="ci-runs__num" to={`/${owner}/${repo}/pipelines/${run.number}`}>
                    #{run.number}
                  </Link>
                </td>
                <td>
                  <CIStatusBadge status={run.status} />
                </td>
                <td className="ci-runs__commit">
                  <span className="ci-runs__msg" title={run.commit_msg || undefined}>
                    {run.commit_msg || "(no commit message)"}
                  </span>
                  <span className="ci-runs__sha muted small" title={run.commit_sha}>
                    {shortSHA(run.commit_sha)}
                    {run.commit_author ? ` · ${run.commit_author}` : ""}
                  </span>
                </td>
                <td className="muted">{shortRef(run.ref)}</td>
                <td className="muted">{run.trigger || "—"}</td>
                <td className="muted">{runDuration(run)}</td>
                <td className="muted" title={absoluteTime(run.created_at)}>
                  {timeAgo(run.created_at)}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  )
}

function CIDisabledCard({ owner, repo }: { owner: string; repo: string }) {
  const setEnabled = useSetCIEnabled(owner, repo)
  return (
    <section className="pipelines">
      <h2 className="pipelines__title">Pipelines</h2>
      <div className="empty">
        <p>
          <strong>CI is disabled for this repository.</strong>
        </p>
        <p className="muted small">
          When enabled, pushing a commit that contains an <code>mgitci.yml</code> runs its pipeline.
          Each job runs in a throwaway container off the configured CI image, so repo-authored
          commands stay isolated from the host.
        </p>
        <button
          type="button"
          className="btn btn--primary"
          onClick={() => setEnabled.mutate(true)}
          disabled={setEnabled.isPending}
        >
          {setEnabled.isPending ? "Enabling…" : "Enable CI"}
        </button>
        {setEnabled.error && (
          <div className="error inline">{(setEnabled.error as Error).message}</div>
        )}
      </div>
    </section>
  )
}

function RunDetail({ owner, repo, runNumber }: { owner: string; repo: string; runNumber: number }) {
  const navigate = useNavigate()
  const runQ = useCIRun(owner, repo, runNumber)
  const rerun = useRerunCIRun(owner, repo)
  const [selectedJob, setSelectedJob] = useState<string | null>(null)

  if (runQ.isLoading) return <div className="loading">Loading…</div>
  if (runQ.error) return <div className="error">{(runQ.error as Error).message}</div>
  if (!runQ.data) return null

  const run = runQ.data
  const jobs = run.jobs
  // Default the open job to the first one that isn't skipped, falling back to
  // the first job; once the user picks one, honor that.
  const activeJob = selectedJob ?? jobs.find((j) => j.status !== "skipped")?.name ?? jobs[0]?.name
  const activeJobObj = jobs.find((j) => j.name === activeJob)

  function doRerun() {
    rerun.mutate(runNumber, {
      onSuccess: (created) => navigate(`/${owner}/${repo}/pipelines/${created.number}`),
    })
  }

  return (
    <section className="pipelines">
      <div className="ci-run-head">
        <Link className="ci-run-head__back" to={`/${owner}/${repo}/pipelines`}>
          ← Pipelines
        </Link>
        <h2 className="pipelines__title">
          Run #{run.number} <CIStatusBadge status={run.status} />
        </h2>
        <button
          type="button"
          className="btn btn--small"
          onClick={doRerun}
          disabled={rerun.isPending}
        >
          {rerun.isPending ? "Re-running…" : "Re-run"}
        </button>
      </div>
      {rerun.error && <div className="error inline">{(rerun.error as Error).message}</div>}

      {run.commit_msg && <p className="ci-run-subject">{run.commit_msg}</p>}

      <dl className="ci-run-meta">
        <div>
          <dt>Commit</dt>
          <dd className="ci-runs__sha" title={run.commit_sha}>
            {shortSHA(run.commit_sha)}
            {run.commit_author ? ` · ${run.commit_author}` : ""}
          </dd>
        </div>
        <div>
          <dt>Ref</dt>
          <dd>{shortRef(run.ref)}</dd>
        </div>
        <div>
          <dt>Trigger</dt>
          <dd>{run.trigger || "—"}</dd>
        </div>
        <div>
          <dt>Duration</dt>
          <dd>{runDuration(run)}</dd>
        </div>
        <div>
          <dt>Started</dt>
          <dd title={run.started_at ? absoluteTime(run.started_at) : ""}>
            {run.started_at ? timeAgo(run.started_at) : "—"}
          </dd>
        </div>
      </dl>

      {jobs.length === 0 ? (
        <div className="empty">No jobs — the run was gated or hasn't started.</div>
      ) : (
        <div className="ci-jobs">
          <JobDag jobs={jobs} active={activeJob} onSelect={setSelectedJob} />
          {activeJobObj && (
            <JobLog
              key={activeJobObj.name}
              owner={owner}
              repo={repo}
              runNumber={runNumber}
              job={activeJobObj}
            />
          )}
        </div>
      )}
    </section>
  )
}

// JobDag renders the run's jobs as the dependency DAG `needs` describes:
// columns by dependency depth, arrows between stages, each job a clickable pill
// that also serves as the log selector. So the structure (build fans out to
// test + vet; web is an independent root) is legible at a glance, not flattened
// into an undifferentiated tab strip.
function JobDag({
  jobs,
  active,
  onSelect,
}: {
  jobs: CIJob[]
  active?: string
  onSelect: (name: string) => void
}) {
  const stages = useMemo(() => jobStages(jobs), [jobs])
  return (
    <nav className="ci-dag" aria-label="Jobs">
      {stages.map((stage, si) => (
        <Fragment key={stage.map((j) => j.name).join("+")}>
          {si > 0 && (
            <div className="ci-dag__arrow" aria-hidden="true">
              →
            </div>
          )}
          <div className="ci-dag__stage">
            {stage.map((j) => {
              const needs = j.needs ?? []
              return (
                <button
                  type="button"
                  key={j.name}
                  className={clsx("ci-dag__job", { "is-active": active === j.name })}
                  onClick={() => onSelect(j.name)}
                  title={needs.length > 0 ? `needs: ${needs.join(", ")}` : "no dependencies"}
                >
                  <CIStatusBadge status={j.status} />
                  <span className="ci-dag__name">{j.name}</span>
                  {needs.length > 0 && <span className="ci-dag__needs">↳ {needs.join(", ")}</span>}
                </button>
              )
            })}
          </div>
        </Fragment>
      ))}
    </nav>
  )
}

// jobStages groups jobs into dependency-depth columns: a root job (no needs) is
// depth 0; any other job sits one past its deepest dependency. The server
// validates the DAG is acyclic, so the recursion terminates. Order within a
// stage follows the jobs' creation (topo) order.
function jobStages(jobs: CIJob[]): CIJob[][] {
  const byName = new Map(jobs.map((j) => [j.name, j]))
  const depth = new Map<string, number>()
  const calc = (name: string): number => {
    const cached = depth.get(name)
    if (cached !== undefined) return cached
    const needs = byName.get(name)?.needs ?? []
    const d = needs.length === 0 ? 0 : 1 + Math.max(...needs.map(calc))
    depth.set(name, d)
    return d
  }
  for (const j of jobs) calc(j.name)
  const maxDepth = jobs.reduce((m, j) => Math.max(m, depth.get(j.name) ?? 0), 0)
  const stages: CIJob[][] = Array.from({ length: maxDepth + 1 }, () => [])
  for (const j of jobs) stages[depth.get(j.name) ?? 0].push(j)
  return stages
}

function JobLog({
  owner,
  repo,
  runNumber,
  job,
}: {
  owner: string
  repo: string
  runNumber: number
  job: CIJob
}) {
  // A skipped job never produced an event stream; don't open a connection.
  const stream = job.status !== "skipped"
  const { events, done, error } = useJobEventStream(owner, repo, runNumber, job.name, stream)
  const steps = useMemo(() => foldSteps(events), [events])

  if (job.status === "skipped") {
    return <div className="empty">Skipped — a dependency didn't succeed.</div>
  }

  return (
    <div className="ci-job-log">
      {error && <div className="error inline">{error}</div>}
      {steps.length === 0 && !done && <div className="loading">Waiting for output…</div>}
      {steps.map((step) => (
        <div className="ci-step" key={step.id}>
          <div className="ci-step__head">
            <span className={`ci-step__status ci-step__status--${step.status ?? "running"}`} />
            <code className="ci-step__cmd">{step.label ?? step.action ?? step.id}</code>
            {step.durationMs !== undefined && (
              <span className="muted small ci-step__dur">{formatDuration(step.durationMs)}</span>
            )}
          </div>
          {step.lines.length > 0 && (
            <pre className="ci-log">
              {step.lines.map((l, i) => (
                // biome-ignore lint/suspicious/noArrayIndexKey: append-only log output, no stable id; line order never changes
                <code key={i} className={l.stream === "stderr" ? "ci-log__stderr" : ""}>
                  {l.text}
                  {"\n"}
                </code>
              ))}
            </pre>
          )}
        </div>
      ))}
    </div>
  )
}

// --- event folding -----------------------------------------------------------

interface StepView {
  id: string
  action?: string
  label?: string
  status?: string
  durationMs?: number
  lines: { stream: string; text: string }[]
}

// foldSteps reduces the flat event stream into per-step views in first-seen
// order: step.started opens a step, step.stdout/stderr append log lines,
// step.completed stamps status + duration. Non-step events (run.started, etc.)
// don't appear in the timeline.
function foldSteps(events: CIEvent[]): StepView[] {
  const byID = new Map<string, StepView>()
  const order: StepView[] = []

  const ensure = (id: string): StepView => {
    let s = byID.get(id)
    if (!s) {
      s = { id, lines: [] }
      byID.set(id, s)
      order.push(s)
    }
    return s
  }

  for (const ev of events) {
    const d = ev.data ?? {}
    const id = typeof d.step_id === "string" ? d.step_id : ""
    switch (ev.type) {
      case "step.started": {
        const s = ensure(id)
        if (typeof d.action === "string") s.action = d.action
        if (typeof d.name === "string") s.label = d.name
        break
      }
      case "step.stdout":
      case "step.stderr": {
        if (!id) break
        const s = ensure(id)
        s.lines.push({
          stream: ev.type === "step.stderr" ? "stderr" : "stdout",
          text: typeof d.line === "string" ? d.line : "",
        })
        break
      }
      case "step.completed": {
        const s = ensure(id)
        if (typeof d.duration_ms === "number") s.durationMs = d.duration_ms
        const result = d.result as { status?: string } | undefined
        if (result && typeof result.status === "string") s.status = result.status
        break
      }
    }
  }
  return order
}

// --- formatting helpers ------------------------------------------------------

function shortSHA(sha: string): string {
  return sha.length > 7 ? sha.slice(0, 7) : sha
}

function shortRef(ref: string): string {
  return ref.replace(/^refs\/heads\//, "").replace(/^refs\/tags\//, "")
}

function runDuration(run: Pick<CIRun, "started_at" | "finished_at" | "status">): string {
  if (!run.started_at) return "—"
  const start = new Date(run.started_at).getTime()
  const end = run.finished_at ? new Date(run.finished_at).getTime() : Date.now()
  return formatDuration(end - start)
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms} ms`
  const s = Math.round(ms / 1000)
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  return `${m}m ${s % 60}s`
}
