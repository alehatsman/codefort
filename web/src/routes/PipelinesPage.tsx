import clsx from "clsx"
import { Fragment, useMemo, useState } from "react"
import { Link, useNavigate, useParams } from "react-router-dom"
import { useCIRun, useCIRuns, useRefs, useRepo } from "../api/queries"
import {
  useCreateAgentTurn,
  useFinishAgentRun,
  useRerunCIRun,
  useSetCIEnabled,
  useTriggerCIRun,
} from "../api/mutations"
import { parseAnsi } from "../lib/ansi"
import { useJobEventStream } from "../lib/ciEvents"
import { absoluteTime, timeAgo } from "../lib/timeAgo"
import type { CIEvent, CIJob, CIRun, CIRunDetail, CIRunExecutionModel, Repo } from "../api/types"
import RepoHeader from "../components/RepoHeader"
import CIStatusBadge from "../components/CIStatusBadge"

// Pipelines tab. One component serves the runs list (/pipelines) and a single
// run's detail (/pipelines/:number), distinguished by the URL — mirroring how
// RepoPage serves the code browser routes.
// kind selects which runs this tab serves: "ci" is the Pipelines tab, "agent"
// is the Agents tab (same components, filtered + relinked).
type RunKind = "ci" | "agent"

export default function PipelinesPage({ kind = "ci" }: { kind?: RunKind }) {
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
        <RunDetail owner={r.owner} repo={r.name} runNumber={runNumber} kind={kind} />
      ) : (
        <RunList repo={r} kind={kind} />
      )}
    </div>
  )
}

// runsBasePath is the route segment a kind's runs live under.
function runsBasePath(kind: RunKind): string {
  return kind === "agent" ? "agents" : "pipelines"
}

// executionModelLabel is the human label for an agent run's execution model
// (#110). Falls back to the default model when the field is absent (older runs).
function executionModelLabel(model: CIRunExecutionModel | undefined): string {
  switch (model) {
    case "mooncake-pilot":
      return "Mooncake pilot"
    default:
      return "Claude (edit)"
  }
}

function RunList({ repo, kind }: { repo: Repo; kind: RunKind }) {
  const { owner, name } = repo
  // Agent runs are spawned from issues regardless of the CI opt-in, so the
  // Agents tab never shows the CI-disabled gate.
  if (kind === "ci" && !repo.ci_enabled) return <CIDisabledCard owner={owner} repo={name} />

  return <EnabledRunList owner={owner} repo={name} kind={kind} />
}

function EnabledRunList({ owner, repo, kind }: { owner: string; repo: string; kind: RunKind }) {
  const runsQ = useCIRuns(owner, repo, kind)
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

  const isAgent = kind === "agent"
  const base = runsBasePath(kind)

  return (
    <section className="pipelines">
      <div className="pipelines__head">
        <h2 className="pipelines__title">{isAgent ? "Agents" : "Pipelines"}</h2>
        {!isAgent && (
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
        )}
      </div>
      <p className="muted small pipelines__lead">
        {isAgent ? (
          <>
            Agent runs are spawned from an issue (the “Spawn agent” button); each works the issue in
            an isolated container.
          </>
        ) : (
          <>
            Runs trigger on push when an <code>mgitci.yml</code> is present at the pushed commit, or
            on demand for any ref above.
          </>
        )}
      </p>
      {trigger.error && <div className="error inline">{(trigger.error as Error).message}</div>}

      {runsQ.isLoading && <div className="loading">Loading…</div>}
      {runsQ.error && <div className="error">{(runsQ.error as Error).message}</div>}
      {runsQ.data && runsQ.data.length === 0 && (
        <div className="empty">
          {isAgent ? (
            <>No agent runs yet. Open an issue and click “Spawn agent” to start one.</>
          ) : (
            <>
              No runs yet. Push a commit with an <code>mgitci.yml</code> to trigger the first one.
            </>
          )}
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
                  <Link className="ci-runs__num" to={`/${owner}/${repo}/${base}/${run.number}`}>
                    #{run.number}
                  </Link>
                  {isAgent && (
                    <span className="ci-runs__model muted small">
                      {executionModelLabel(run.execution_model)}
                    </span>
                  )}
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

function RunDetail({
  owner,
  repo,
  runNumber,
  kind,
}: {
  owner: string
  repo: string
  runNumber: number
  kind: RunKind
}) {
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
  const base = runsBasePath(kind)
  const isAgent = kind === "agent"

  function doRerun() {
    rerun.mutate(runNumber, {
      onSuccess: (created) => navigate(`/${owner}/${repo}/${base}/${created.number}`),
    })
  }

  return (
    <section className="pipelines">
      <div className="ci-run-head">
        <Link className="ci-run-head__back" to={`/${owner}/${repo}/${base}`}>
          ← {isAgent ? "Agents" : "Pipelines"}
        </Link>
        <h2 className="pipelines__title">
          Run #{run.number} <CIStatusBadge status={run.status} />
        </h2>
        {/* Re-run re-enqueues a CI run, which is meaningless for an agent run. */}
        {!isAgent && (
          <button
            type="button"
            className="btn btn--small"
            onClick={doRerun}
            disabled={rerun.isPending}
          >
            {rerun.isPending ? "Re-running…" : "Re-run"}
          </button>
        )}
      </div>
      {rerun.error && <div className="error inline">{(rerun.error as Error).message}</div>}

      {run.commit_msg && <p className="ci-run-subject">{run.commit_msg}</p>}

      <dl className="ci-run-meta">
        <div>
          <dt>Commit</dt>
          <dd className="ci-runs__sha" title={run.commit_sha}>
            {run.commit_sha ? (
              <Link to={`/${owner}/${repo}/commit/${run.commit_sha}`}>
                {shortSHA(run.commit_sha)}
              </Link>
            ) : (
              shortSHA(run.commit_sha)
            )}
            {run.commit_author ? ` · ${run.commit_author}` : ""}
          </dd>
        </div>
        <div>
          <dt>Ref</dt>
          <dd>{shortRef(run.ref)}</dd>
        </div>
        {isAgent && (
          <div>
            <dt>Model</dt>
            <dd>
              {executionModelLabel(run.execution_model)}
              {run.execution_model === "mooncake-pilot" && (
                <span className="muted small">
                  {" "}
                  · shell {run.pilot_allow_shell ? "allowed" : "denied"}
                </span>
              )}
            </dd>
          </div>
        )}
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

      {run.kind === "agent" ? (
        <>
          <AgentTranscript owner={owner} repo={repo} runNumber={runNumber} />
          <AgentMessageBox owner={owner} repo={repo} runNumber={runNumber} run={run} />
        </>
      ) : jobs.length === 0 ? (
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
      {steps.map((step) => {
        const status = step.status ?? "running"
        const failed = status === "failed" || status === "error"
        return (
          <div className="ci-step" key={step.id}>
            <div className={clsx("ci-step__head", { "ci-step__head--failed": failed })}>
              <span className={`ci-step__status ci-step__status--${status}`} />
              <code className="ci-step__cmd">{step.label ?? step.action ?? step.id}</code>
              {step.durationMs !== undefined && (
                <span className="muted small ci-step__dur">{formatDuration(step.durationMs)}</span>
              )}
            </div>
            {step.lines.length > 0 && (
              <pre className="ci-log">
                {step.lines.map((l, i) => (
                  <code
                    // biome-ignore lint/suspicious/noArrayIndexKey: append-only log output, no stable id; line order never changes
                    key={i}
                    className={clsx("ci-log__line", {
                      "ci-log__line--stderr": l.stream === "stderr",
                    })}
                  >
                    <LogLine text={l.text} />
                    {"\n"}
                  </code>
                ))}
              </pre>
            )}
          </div>
        )
      })}
    </div>
  )
}

// AgentTranscript renders an agent run's live transcript. The agent run has a
// single "agent" job whose event stream carries agent.* events (one per claude
// stream-json line); we fold them into readable entries and render in order.
// The same SSE consumer as CI handles replay + resume, so a terminal run
// replays its whole transcript and a live one tails it.
function AgentTranscript({
  owner,
  repo,
  runNumber,
}: {
  owner: string
  repo: string
  runNumber: number
}) {
  const { events, done, error } = useJobEventStream(owner, repo, runNumber, "agent", true)
  const entries = useMemo(() => foldAgentEvents(events), [events])

  return (
    <div className="agent-transcript">
      {error && <div className="error inline">{error}</div>}
      {entries.length === 0 && !done && <div className="loading">Waiting for the agent…</div>}
      {entries.map((e, i) => (
        <div key={i} className={clsx("agent-entry", `agent-entry--${e.kind}`)}>
          {e.label && <div className="agent-entry__label">{e.label}</div>}
          <pre className="agent-entry__body">{e.text}</pre>
        </div>
      ))}
    </div>
  )
}

// AgentMessageBox lets a human send follow-up turns to an agent run. It's live
// while the run isn't terminal: a message sent mid-turn queues behind the
// current one (the server accepts it; the dispatch loop runs it next).
function AgentMessageBox({
  owner,
  repo,
  runNumber,
  run,
}: {
  owner: string
  repo: string
  runNumber: number
  run: CIRunDetail
}) {
  const [text, setText] = useState("")
  const send = useCreateAgentTurn(owner, repo, runNumber)
  const finish = useFinishAgentRun(owner, repo, runNumber)
  const terminal = ["success", "failed", "canceled", "error"].includes(run.status)
  const queued = (run.turns ?? []).filter((t) => t.status === "pending" || t.status === "running")

  if (terminal) {
    return (
      <div className="agent-msgbox agent-msgbox--done muted small">
        This agent run has finished
        {run.status === "success" ? " — see the issue for the result branch." : "."}
      </div>
    )
  }

  if (run.status === "finishing") {
    return (
      <div className="agent-msgbox agent-msgbox--done muted small">
        Finishing — handing off the result…
      </div>
    )
  }

  function submit(e: React.FormEvent) {
    e.preventDefault()
    const t = text.trim()
    if (!t) return
    send.mutate(t, { onSuccess: () => setText("") })
  }

  return (
    <form className="agent-msgbox" onSubmit={submit}>
      {queued.length > 0 && (
        <div className="muted small">
          {queued.length} message{queued.length > 1 ? "s" : ""} queued…
        </div>
      )}
      <textarea
        className="input agent-msgbox__input"
        value={text}
        onChange={(e) => setText(e.target.value)}
        placeholder={
          run.status === "running"
            ? "Agent is working — your message will queue…"
            : "Send the agent a message…"
        }
        aria-label="Message to the agent"
        rows={3}
      />
      <div className="agent-msgbox__actions">
        <button
          type="button"
          className="btn btn--small"
          onClick={() => finish.mutate()}
          disabled={finish.isPending || run.status === "running"}
          title="Hand off the agent's work: push agent/issue-N and comment on the issue"
        >
          {finish.isPending ? "Finishing…" : "Finish"}
        </button>
        <button
          type="submit"
          className="btn btn--small btn--primary"
          disabled={send.isPending || text.trim() === ""}
        >
          {send.isPending ? "Sending…" : "Send"}
        </button>
      </div>
      {send.error && <div className="error inline">{(send.error as Error).message}</div>}
      {finish.error && <div className="error inline">{(finish.error as Error).message}</div>}
    </form>
  )
}

interface AgentEntry {
  kind: "turn" | "thinking" | "assistant" | "tool_use" | "tool_result" | "result" | "system" | "raw"
  label?: string
  text: string
}

// foldAgentEvents reduces the agent event stream into display entries. Each
// agent.message carries the raw claude stream-json object under data.claude; we
// pull out the human-meaningful parts (assistant text, tool calls + results,
// the final result) and fall back to compact JSON for anything unrecognized, so
// the transcript stays faithful even as Claude's schema evolves.
function foldAgentEvents(events: CIEvent[]): AgentEntry[] {
  const out: AgentEntry[] = []

  for (const ev of events) {
    const d = ev.data ?? {}
    switch (ev.type) {
      case "agent.turn.started": {
        const turn = typeof d.turn === "number" ? d.turn : "?"
        out.push({ kind: "turn", label: `Turn ${turn}`, text: asString(d.prompt) })
        break
      }
      case "agent.turn.completed": {
        const status = typeof d.status === "string" ? d.status : "done"
        const bits = [`status: ${status}`]
        if (typeof d.num_turns === "number") bits.push(`${d.num_turns} steps`)
        if (typeof d.duration_ms === "number") bits.push(formatDuration(d.duration_ms))
        if (typeof d.cost_usd === "number" && d.cost_usd > 0) bits.push(`$${d.cost_usd.toFixed(4)}`)
        out.push({ kind: "result", label: "Turn complete", text: bits.join(" · ") })
        break
      }
      case "agent.raw":
        out.push({ kind: "raw", text: asString(d.line) })
        break
      case "agent.message":
        out.push(...foldClaudeMessage(d.claude as Record<string, unknown> | undefined))
        break
    }
  }
  return out
}

// foldClaudeMessage turns one claude stream-json object into zero or more
// transcript entries.
function foldClaudeMessage(obj: Record<string, unknown> | undefined): AgentEntry[] {
  if (!obj || typeof obj.type !== "string") return []
  switch (obj.type) {
    case "system":
      return [{ kind: "system", label: "session", text: asString(obj.model ?? obj.subtype) }]
    case "assistant":
    case "user":
      return foldMessageContent(obj)
    case "result":
      return [] // summarized by agent.turn.completed
    default:
      return []
  }
}

function foldMessageContent(obj: Record<string, unknown>): AgentEntry[] {
  const message = obj.message as { content?: unknown } | undefined
  const content = message?.content
  if (!Array.isArray(content)) return []
  const out: AgentEntry[] = []
  for (const block of content) {
    if (!block || typeof block !== "object") continue
    const b = block as Record<string, unknown>
    switch (b.type) {
      case "text":
        out.push({ kind: "assistant", text: asString(b.text) })
        break
      case "thinking":
        out.push({ kind: "thinking", label: "thinking", text: asString(b.thinking) })
        break
      case "tool_use":
        out.push({ kind: "tool_use", label: `🔧 ${asString(b.name)}`, text: compactJSON(b.input) })
        break
      case "tool_result":
        out.push({ kind: "tool_result", label: "result", text: asString(b.content) })
        break
    }
  }
  return out
}

function asString(v: unknown): string {
  if (typeof v === "string") return v
  if (v === undefined || v === null) return ""
  return compactJSON(v)
}

function compactJSON(v: unknown): string {
  try {
    return JSON.stringify(v, null, 2)
  } catch {
    return String(v)
  }
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
