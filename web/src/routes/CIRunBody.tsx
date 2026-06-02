import clsx from "clsx"
import { Fragment, useMemo, useState } from "react"
import CIStatusBadge from "../components/CIStatusBadge"
import type { CIEvent, CIJob } from "../api/types"
import { parseAnsi } from "../lib/ansi"
import { EmptyState, Spinner } from "../components/ui"
import { useJobEventStream } from "../lib/ciEvents"
import { formatDuration } from "./runHelpers"

// CIRunBody renders a CI run's jobs: the dependency DAG and the selected job's
// live log. It owns the job-selection state (CI-only); the shared run header
// lives in the parent. An agent run renders AgentRunBody instead.
export default function CIRunBody({
  owner,
  repo,
  runNumber,
  jobs,
}: {
  owner: string
  repo: string
  runNumber: number
  jobs: CIJob[]
}) {
  const [selectedJob, setSelectedJob] = useState<string | null>(null)

  if (jobs.length === 0) {
    return <EmptyState>No jobs — the run was gated or hasn't started.</EmptyState>
  }

  // Default the open job to the first one that isn't skipped, falling back to
  // the first job; once the user picks one, honor that.
  const activeJob = selectedJob ?? jobs.find((j) => j.status !== "skipped")?.name ?? jobs[0]?.name
  const activeJobObj = jobs.find((j) => j.name === activeJob)

  return (
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
                  className={clsx("ci-dag__job", `ci-dag__job--${j.status}`, {
                    "is-active": active === j.name,
                  })}
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
    return <EmptyState>Skipped — a dependency didn't succeed.</EmptyState>
  }

  return (
    <div className="ci-job-log">
      {error && <div className="error inline">{error}</div>}
      {steps.length === 0 && !done && <Spinner label="Waiting for output…" />}
      {steps.map((step) => {
        const status = step.status ?? "running"
        const failed = status === "failed" || status === "error"
        return (
          <div className="ci-step" key={step.id}>
            <div className={clsx("ci-step__head", { "ci-step__head--failed": failed })}>
              <span className={`ci-step__status ci-step__status--${status}`} />
              <span className="ci-step__cmd">{step.label ?? step.action ?? step.id}</span>
              {step.durationMs !== undefined && (
                <span className="muted small ci-step__dur">{formatDuration(step.durationMs)}</span>
              )}
            </div>
            {step.lines.length > 0 && (
              <div className="ci-log">
                {step.lines.map((l, i) => (
                  <span
                    // biome-ignore lint/suspicious/noArrayIndexKey: append-only log output, no stable id; line order never changes
                    key={i}
                    className={clsx("ci-log__line", {
                      "ci-log__line--stderr": l.stream === "stderr",
                    })}
                  >
                    <LogLine text={l.text} />
                    {"\n"}
                  </span>
                ))}
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}

// LogLine renders one log line, interpreting ANSI SGR color escapes into spans.
// A line with no escapes is a single plain segment, so the common case stays a
// bare text node.
function LogLine({ text }: { text: string }) {
  const segments = parseAnsi(text)
  if (segments.length === 1 && !segments[0].fg && !segments[0].bold && !segments[0].underline) {
    return <>{segments[0].text}</>
  }
  return (
    <>
      {segments.map((seg, i) => (
        <span
          // biome-ignore lint/suspicious/noArrayIndexKey: segments derive from a fixed line; order is stable
          key={i}
          className={clsx(seg.fg && `ansi-fg--${seg.fg}`, {
            "ansi-bold": seg.bold,
            "ansi-dim": seg.dim,
            "ansi-underline": seg.underline,
          })}
        >
          {seg.text}
        </span>
      ))}
    </>
  )
}

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
