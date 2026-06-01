import clsx from "clsx"
import { useEffect, useMemo, useRef, useState } from "react"
import { useCancelAgentRun, useCreateAgentTurn, useFinishAgentRun } from "../api/mutations"
import type { CIEvent, CIRunDetail } from "../api/types"
import { useJobEventStream } from "../lib/ciEvents"
import { formatDuration } from "./runHelpers"

// Terminal agent-run statuses: no further turns, the message box closes.
const TERMINAL = ["success", "failed", "canceled", "error", "interrupted"]

// AgentRunBody renders an agent run's live transcript plus the follow-up
// message box. A CI run renders CIRunBody (the job DAG) instead; both hang off
// the shared run header in the parent.
export default function AgentRunBody({
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
  return (
    <>
      <AgentTranscript owner={owner} repo={repo} runNumber={runNumber} run={run} />
      <AgentMessageBox owner={owner} repo={repo} runNumber={runNumber} run={run} />
    </>
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
  run,
}: {
  owner: string
  repo: string
  runNumber: number
  run: CIRunDetail
}) {
  // The server ends the event stream when a turn parks at awaiting_input (so the
  // response is finite and flushes through a buffering proxy/tunnel). When the
  // run resumes for another turn (awaiting_input -> running/finishing), bump the
  // resubscribe key to re-open the stream and append the new turn. useCIRun
  // polls run.status, so this fires within a poll of the resume.
  const [resumeKey, setResumeKey] = useState(0)
  const prevStatus = useRef(run.status)
  useEffect(() => {
    const prev = prevStatus.current
    prevStatus.current = run.status
    if (prev === "awaiting_input" && (run.status === "running" || run.status === "finishing")) {
      setResumeKey((k) => k + 1)
    }
  }, [run.status])

  const { events, done, error } = useJobEventStream(
    owner,
    repo,
    runNumber,
    "agent",
    true,
    resumeKey
  )
  const entries = useMemo(() => foldAgentEvents(events), [events])

  // A turn is in flight while its agent.turn.started has no matching
  // turn.completed. During that window the agent is busy but emits nothing
  // while it plans (mooncake-pilot runs Claude as a one-shot planner, then
  // bursts the steps), so without a hint the silent gap reads as a hang.
  // "planning" until the turn produces its first entry, "working" once
  // steps/output begin. Gated on the run's terminal status (not the stream's
  // `done`, which also closes on error) so a parked awaiting_input run — turn
  // already completed — correctly shows nothing.
  const terminal = TERMINAL.includes(run.status)
  const phase = useMemo<"planning" | "working" | null>(() => {
    if (terminal || error) return null
    let started = 0
    let completed = 0
    for (const e of events) {
      if (e.type === "agent.turn.started") started++
      else if (e.type === "agent.turn.completed") completed++
    }
    if (started <= completed) return null
    const lastTurn = entries.map((e) => e.kind).lastIndexOf("turn")
    return lastTurn >= 0 && lastTurn < entries.length - 1 ? "working" : "planning"
  }, [events, entries, terminal, error])

  return (
    <div className="agent-transcript">
      {error && <div className="error inline">{error}</div>}
      {entries.length === 0 && !done && <div className="loading">Waiting for the agent…</div>}
      {entries.map((e) =>
        e.kind === "step" ? (
          <div key={e.id} className={clsx("agent-step", `agent-step--${e.status}`)}>
            <div className="agent-step__line">
              <span className="agent-step__glyph" aria-hidden="true">
                {STEP_GLYPH[e.status ?? "running"]}
              </span>
              <span className="agent-step__name">{e.label}</span>
            </div>
            {e.text && <pre className="agent-step__detail">{e.text}</pre>}
          </div>
        ) : (
          <div key={e.id} className={clsx("agent-entry", `agent-entry--${e.kind}`)}>
            {e.label && <div className="agent-entry__label">{e.label}</div>}
            {e.text && <pre className="agent-entry__body">{e.text}</pre>}
          </div>
        )
      )}
      {phase && (
        <div className={clsx("agent-working", `agent-working--${phase}`)} aria-live="polite">
          <span className="agent-working__dot" aria-hidden="true" />
          {phase === "planning" ? "Planning…" : "Working…"}
        </div>
      )}
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
  const cancel = useCancelAgentRun(owner, repo, runNumber)
  const terminal = TERMINAL.includes(run.status)
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
          type="button"
          className="btn btn--small btn--danger"
          onClick={() => cancel.mutate()}
          disabled={cancel.isPending}
          title="Force-stop the run now: interrupt the agent and discard the workspace (no branch is handed off)"
        >
          {cancel.isPending ? "Stopping…" : "Stop"}
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
      {cancel.error && <div className="error inline">{(cancel.error as Error).message}</div>}
    </form>
  )
}

// Step status drives the terminal glyph (▶ running → ✓/~/✗/⊘) and color,
// mirroring mooncake's own renderers (console_logger.go / agentd runs.go).
type StepStatus = "running" | "ok" | "changed" | "failed" | "skipped"

const STEP_GLYPH: Record<StepStatus, string> = {
  running: "▶",
  ok: "✓",
  changed: "~",
  failed: "✗",
  skipped: "⊘",
}

interface AgentEntry {
  // id is a stable React key, assigned by foldAgentEvents from the source
  // event's seq + a per-event sub-index. The fold helpers don't set it.
  id?: string
  kind:
    | "step"
    | "turn"
    | "thinking"
    | "assistant"
    | "tool_use"
    | "tool_result"
    | "result"
    | "system"
    | "raw"
  // status is set only on "step" entries; it's mutated in place when the step
  // resolves so the live row flips ▶ → ✓/~/✗ without spawning a second line.
  status?: StepStatus
  label?: string
  text: string
}

// foldAgentEvents reduces the agent event stream into display entries. An
// agent.message carries either a claude stream-json object under data.claude
// (claude-edit runs) or a mooncake NDJSON event under data.mooncake
// (mooncake-pilot runs); we fold each into human-meaningful entries and fall
// back to compact JSON for anything unrecognized, so the transcript stays
// faithful even as either schema evolves.
function foldAgentEvents(events: CIEvent[]): AgentEntry[] {
  const out: AgentEntry[] = []
  // mooncake-pilot streams each step as separate started/stdout/completed
  // events; accumulate per-step output to emit one entry per completed step.
  const pilot: PilotState = { steps: new Map() }

  for (const ev of events) {
    const d = ev.data ?? {}
    // Tag every entry this event produces with a key stable across appends and
    // deterministic re-folds: the event seq plus its position within the event.
    const start = out.length
    switch (ev.type) {
      case "agent.turn.started": {
        const turn = typeof d.turn === "number" ? d.turn : "?"
        out.push({ kind: "turn", label: `Turn ${turn}`, text: asString(d.prompt) })
        break
      }
      case "agent.turn.completed": {
        const status = typeof d.status === "string" ? d.status : "done"
        const bits = [`status: ${status}`]
        if (typeof d.num_turns === "number" && d.num_turns > 0) bits.push(`${d.num_turns} steps`)
        if (typeof d.duration_ms === "number" && d.duration_ms > 0)
          bits.push(formatDuration(d.duration_ms))
        if (typeof d.cost_usd === "number" && d.cost_usd > 0) bits.push(`$${d.cost_usd.toFixed(4)}`)
        out.push({ kind: "result", label: "Turn complete", text: bits.join(" · ") })
        break
      }
      case "agent.raw":
        out.push({ kind: "raw", text: asString(d.line) })
        break
      case "agent.message":
        if (d.mooncake) {
          out.push(...foldMooncakeEvent(d.mooncake as Record<string, unknown>, pilot, out))
        } else {
          out.push(...foldClaudeMessage(d.claude as Record<string, unknown> | undefined))
        }
        break
    }
    for (let i = start; i < out.length; i++) out[i].id = `${ev.seq}.${i - start}`
  }
  return out
}

// PilotState carries the in-flight mooncake steps across the fold: a step's
// output (stdout/stderr/file changes) streams between its started and completed
// events, so we buffer it keyed by step_id (last = the step in flight, for
// events like file.created that omit step_id).
interface PilotState {
  // index is the step's row position in the fold's `out` array, so a later
  // step.completed mutates the same row the step.started pushed (the live ▶
  // line flips in place rather than appending a second row).
  steps: Map<string, { action?: string; name?: string; lines: string[]; index: number }>
  last?: string
}

// foldMooncakeEvent turns one mooncake NDJSON event (data.mooncake on a
// mooncake-pilot run's agent.message) into transcript entries, mirroring
// mooncake's own terminal renderer: each step is one line whose glyph flips
// ▶ → ✓/~/✗ in place. step.started pushes the live row into `out` and records
// its index; the matching step.completed mutates that same row rather than
// appending a second line. Output streams across step.stdout/file.* events and
// is buffered, but kept only to surface under a *failed* step (success rows
// show just the name). run.completed renders the mooncake RECAP line.
function foldMooncakeEvent(
  m: Record<string, unknown>,
  pilot: PilotState,
  out: AgentEntry[]
): AgentEntry[] {
  const type = typeof m.type === "string" ? m.type : ""
  const data = (m.data as Record<string, unknown>) ?? {}

  switch (type) {
    case "plan.loaded": {
      const n = data.total_steps
      return [
        { kind: "system", label: typeof n === "number" ? `plan · ${n} steps` : "plan", text: "" },
      ]
    }
    case "step.started": {
      const id = asString(data.step_id)
      const name = typeof data.name === "string" ? data.name : ""
      // Push the live row now (▶) and remember where it sits so step.completed
      // can flip it in place.
      const index = out.length
      out.push({ kind: "step", status: "running", label: name || id || "step", text: "" })
      pilot.steps.set(id, {
        action: typeof data.action === "string" ? data.action : undefined,
        name: name || undefined,
        lines: [],
        index,
      })
      pilot.last = id
      return []
    }
    case "step.stdout":
    case "step.stderr": {
      const id = data.step_id ? asString(data.step_id) : pilot.last
      const step = id ? pilot.steps.get(id) : undefined
      if (step) step.lines.push(asString(data.line))
      return []
    }
    case "file.created":
    case "file.modified":
    case "file.deleted": {
      // file.* carries no step_id; attach to the step in flight.
      const step = pilot.last ? pilot.steps.get(pilot.last) : undefined
      if (step) step.lines.push(`${type.slice("file.".length)} ${asString(data.path)}`)
      return []
    }
    case "step.completed":
    case "step.failed":
    case "step.skipped": {
      const id = asString(data.step_id)
      const tracked = pilot.steps.get(id)
      pilot.steps.delete(id)
      const result = (data.result as Record<string, unknown>) ?? {}
      const action = tracked?.action ?? (typeof data.action === "string" ? data.action : "")
      // Resolve the terminal status from the event type or the result payload.
      const status: StepStatus =
        type === "step.failed" || result.failed || result.status === "failed"
          ? "failed"
          : type === "step.skipped" || result.status === "skipped"
            ? "skipped"
            : result.status === "changed"
              ? "changed"
              : "ok"
      const row = tracked ? out[tracked.index] : undefined
      if (!row) return []
      row.status = status
      // Details surface only on failure — keep the success log a clean list.
      if (status === "failed") {
        const lines: string[] = []
        // For cmd/shell steps the executed command rides result.target (the
        // rendered argv); lead with it as a `$ …` line so a failure shows what
        // actually ran. Other actions put a path/package in target — skip it.
        const cmdline = action === "cmd" || action === "shell" ? asString(result.target) : ""
        if (cmdline) lines.push(`$ ${cmdline}`)
        lines.push(...(tracked?.lines ?? []))
        const err = asString(result.error) || asString(data.error_message)
        if (err) lines.push(err)
        row.text = lines.join("\n")
      }
      return []
    }
    case "run.completed": {
      const num = (k: string) => (typeof data[k] === "number" ? (data[k] as number) : 0)
      const bits = [`ok=${num("success_steps")}`, `changed=${num("changed_steps")}`]
      if (num("skipped_steps") > 0) bits.push(`skipped=${num("skipped_steps")}`)
      bits.push(`failed=${num("failed_steps")}`)
      if (num("duration_ms") > 0) bits.push(formatDuration(num("duration_ms")))
      return [{ kind: "result", label: "RECAP", text: bits.join("  ") }]
    }
    case "pilot.completed": {
      // The pilot wraps one or more plan/execute loops; status + stop_reason
      // already ride the "Turn complete" line, but the iteration count is shown
      // nowhere else. Surface it only when the pilot actually re-planned (>1) or
      // stopped for a non-success reason — otherwise it's noise.
      const iterations = typeof data.iterations === "number" ? data.iterations : 0
      const stop = typeof data.stop_reason === "string" ? data.stop_reason : ""
      const bits: string[] = []
      if (iterations > 1) bits.push(`${iterations} iterations`)
      if (stop && stop !== "success") bits.push(stop)
      if (bits.length === 0) return []
      return [{ kind: "system", label: "pilot", text: bits.join(" · ") }]
    }
    default:
      // run.started, etc — redundant with the above / the turn line.
      return []
  }
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
        // Compact, like a terminal action line — the name is the signal; the
        // input args are dropped to keep the log scannable (details-on-failure).
        out.push({ kind: "tool_use", label: `🔧 ${asString(b.name)}`, text: "" })
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
