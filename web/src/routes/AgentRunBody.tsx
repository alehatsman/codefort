import { useVirtualizer } from "@tanstack/react-virtual"
import clsx from "clsx"
import { Fragment, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react"
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

  // Group the flat entries into CI-style cards: a "turn" or "plan" entry opens a
  // card (its label is the card title) and every following non-header entry
  // (prompt, steps, recap) is its body — so the transcript reads as a stack of
  // cards ("Turn 1" with the prompt, "plan · N steps" with its steps + recap),
  // matching the CI pipeline's step cards.
  const cards = useMemo(() => buildCards(entries), [entries])

  // A turn is in flight while its agent.turn.started has no matching
  // turn.completed. During that window the agent is busy but emits nothing
  // while it plans (mooncake-agent runs Claude as a one-shot planner, then
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

  // The transcript is a bounded, windowed scroll region: only the on-screen
  // cards are mounted (a long-running session accrues many turns), and it
  // auto-follows the bottom like a terminal tail so the latest step is always in
  // view without the page growing unbounded. Cards vary in height, so the
  // virtualizer measures each rendered card rather than assuming a fixed size.
  const scrollRef = useRef<HTMLDivElement>(null)
  const virtualizer = useVirtualizer({
    count: cards.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => 96,
    overscan: 6,
    getItemKey: (i) => cards[i].id,
  })

  // "Stuck" = the viewport is at (or near) the end, so we keep pinning to the
  // bottom as rows arrive. It starts true (open pinned to the latest line) and
  // flips off the moment the user scrolls up to read history — then a "jump to
  // latest" control re-engages it. A ref mirrors it for the layout effect, which
  // must read the live value without being a dependency.
  const [stuck, setStuck] = useState(true)
  const stuckRef = useRef(true)

  const onScroll = useCallback(() => {
    const el = scrollRef.current
    if (!el) return
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 32
    stuckRef.current = atBottom
    setStuck(atBottom)
  }, [])

  const jumpToLatest = useCallback(() => {
    const el = scrollRef.current
    if (!el) return
    stuckRef.current = true
    setStuck(true)
    el.scrollTop = el.scrollHeight
  }, [])

  // Pin to the bottom while stuck. Keyed on the row count, the measured total
  // size, and the phase line so it re-pins as content grows and as rows settle
  // to their real heights (dynamic measurement changes the total post-paint).
  const total = virtualizer.getTotalSize()
  // biome-ignore lint/correctness/useExhaustiveDependencies: total/phase aren't read in the body — they're the intentional re-pin triggers (content grew or cards re-measured), so the effect must re-run when they change.
  useLayoutEffect(() => {
    const el = scrollRef.current
    if (!el || !stuckRef.current) return
    el.scrollTop = el.scrollHeight
  }, [cards.length, total, phase])

  return (
    <div className="agent-transcript-wrap">
      {error && <div className="error inline">{error}</div>}
      {cards.length === 0 && !done && <div className="loading">Waiting for the agent…</div>}
      <div ref={scrollRef} className="agent-transcript" onScroll={onScroll}>
        <div className="agent-transcript__sizer" style={{ height: total }}>
          {virtualizer.getVirtualItems().map((vi) => (
            <div
              key={vi.key}
              data-index={vi.index}
              ref={virtualizer.measureElement}
              className="agent-transcript__row"
              style={{ transform: `translateY(${vi.start}px)` }}
            >
              {renderCard(cards[vi.index])}
            </div>
          ))}
        </div>
        {phase && (
          <div className={clsx("agent-working", `agent-working--${phase}`)} aria-live="polite">
            <span className="agent-working__dot" aria-hidden="true" />
            {phase === "planning" ? "Planning…" : "Working…"}
          </div>
        )}
      </div>
      {!stuck && (
        <button type="button" className="agent-follow-btn" onClick={jumpToLatest}>
          ↓ Jump to latest
        </button>
      )}
    </div>
  )
}

// A Card is one section of the transcript — a turn or a plan — rendered with
// the CI pipeline's step-card chrome: a head (title + status dot) over a body
// of entries (the prompt, the steps, the recap).
interface Card {
  id: string
  title: string
  // status drives the head dot / --failed tint, derived from the body's steps;
  // undefined for a card with no steps (e.g. a bare turn header).
  status?: "ok" | "failed" | "running"
  body: AgentEntry[]
}

// buildCards groups the flat entries into cards. A "turn" or a "plan" system
// entry opens a card (its label becomes the title); every following non-header
// entry is appended to that card's body. A turn's prompt rides the turn entry's
// text, so it's pushed into the body as prose.
function buildCards(entries: AgentEntry[]): Card[] {
  const isHeader = (e: AgentEntry) =>
    e.kind === "turn" || (e.kind === "system" && (e.label ?? "").startsWith("plan"))
  const cards: Card[] = []
  let cur: Card | null = null
  for (const e of entries) {
    if (isHeader(e)) {
      cur = { id: e.id ?? `c${cards.length}`, title: e.label ?? "", body: [] }
      cards.push(cur)
      if (e.kind === "turn" && e.text) {
        cur.body.push({ id: `${cur.id}-prompt`, kind: "assistant", text: e.text })
      }
      continue
    }
    if (!cur) {
      cur = { id: e.id ?? `c${cards.length}`, title: "", body: [] }
      cards.push(cur)
    }
    cur.body.push(e)
  }
  // Derive each card's head status from its steps: failed wins, then running,
  // else ok if it ran any step at all.
  for (const c of cards) {
    for (const b of c.body) {
      if (b.kind !== "step") continue
      if (b.status === "failed") {
        c.status = "failed"
        break
      }
      if (b.status === "running") c.status = "running"
      else if (c.status !== "running") c.status = "ok"
    }
  }
  return cards
}

// renderCard draws one section as a CI-style step card: a head (status dot +
// title) over a body of rendered entries.
function renderCard(card: Card) {
  return (
    <div className="ci-step agent-card">
      <div className={clsx("ci-step__head", { "ci-step__head--failed": card.status === "failed" })}>
        {card.status && <span className={`ci-step__status ci-step__status--${card.status}`} />}
        <code className="ci-step__cmd">{card.title}</code>
      </div>
      {card.body.length > 0 && (
        <div className="ci-log agent-card__body">
          {card.body.map((e) => (
            <Fragment key={e.id}>{renderEntry(e)}</Fragment>
          ))}
        </div>
      )}
    </div>
  )
}

// renderEntry draws one body entry: a terminal-style step line (glyph + name,
// failure detail below) or a text entry (prompt / assistant / thinking / recap).
function renderEntry(e: AgentEntry) {
  if (e.kind === "step") {
    return (
      <div className={clsx("agent-step", `agent-step--${e.status}`)}>
        <div className="agent-step__line">
          <span className="agent-step__glyph" aria-hidden="true">
            {STEP_GLYPH[e.status ?? "running"]}
          </span>
          <span className="agent-step__name">{e.label}</span>
        </div>
        {e.text && <pre className="agent-step__detail">{e.text}</pre>}
      </div>
    )
  }
  return (
    <div className={clsx("agent-entry", `agent-entry--${e.kind}`)}>
      {e.label && <div className="agent-entry__label">{e.label}</div>}
      {e.text && <pre className="agent-entry__body">{e.text}</pre>}
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
// (mooncake-agent runs); we fold each into human-meaningful entries and fall
// back to compact JSON for anything unrecognized, so the transcript stays
// faithful even as either schema evolves.
function foldAgentEvents(events: CIEvent[]): AgentEntry[] {
  const out: AgentEntry[] = []
  // mooncake-agent streams each step as separate started/stdout/completed
  // events; accumulate per-step output to emit one entry per completed step.
  const mc: MooncakeState = { steps: new Map() }

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
          out.push(...foldMooncakeEvent(d.mooncake as Record<string, unknown>, mc, out))
        } else {
          out.push(...foldClaudeMessage(d.claude as Record<string, unknown> | undefined))
        }
        break
    }
    for (let i = start; i < out.length; i++) out[i].id = `${ev.seq}.${i - start}`
  }
  return out
}

// MooncakeState carries the in-flight mooncake steps across the fold: a step's
// output (stdout/stderr/file changes) streams between its started and completed
// events, so we buffer it keyed by step_id (last = the step in flight, for
// events like file.created that omit step_id).
interface MooncakeState {
  // index is the step's row position in the fold's `out` array, so a later
  // step.completed mutates the same row the step.started pushed (the live ▶
  // line flips in place rather than appending a second row).
  steps: Map<string, { action?: string; name?: string; lines: string[]; index: number }>
  last?: string
}

// foldMooncakeEvent turns one mooncake NDJSON event (data.mooncake on a
// mooncake-agent run's agent.message) into transcript entries, mirroring
// mooncake's own terminal renderer: each step is one line whose glyph flips
// ▶ → ✓/~/✗ in place. step.started pushes the live row into `out` and records
// its index; the matching step.completed mutates that same row rather than
// appending a second line. Output streams across step.stdout/file.* events and
// is buffered, but kept only to surface under a *failed* step (success rows
// show just the name). run.completed renders the mooncake RECAP line.
function foldMooncakeEvent(
  m: Record<string, unknown>,
  mc: MooncakeState,
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
      mc.steps.set(id, {
        action: typeof data.action === "string" ? data.action : undefined,
        name: name || undefined,
        lines: [],
        index,
      })
      mc.last = id
      return []
    }
    case "step.stdout":
    case "step.stderr": {
      const id = data.step_id ? asString(data.step_id) : mc.last
      const step = id ? mc.steps.get(id) : undefined
      if (step) step.lines.push(asString(data.line))
      return []
    }
    case "file.created":
    case "file.modified":
    case "file.deleted": {
      // file.* carries no step_id; attach to the step in flight.
      const step = mc.last ? mc.steps.get(mc.last) : undefined
      if (step) step.lines.push(`${type.slice("file.".length)} ${asString(data.path)}`)
      return []
    }
    case "step.completed":
    case "step.failed":
    case "step.skipped": {
      const id = asString(data.step_id)
      const tracked = mc.steps.get(id)
      mc.steps.delete(id)
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
    case "agent.completed": {
      // The mooncake agent wraps one or more plan/execute loops; status +
      // stop_reason already ride the "Turn complete" line, but the iteration
      // count is shown nowhere else. Surface it only when it actually
      // re-planned (>1) or stopped for a non-success reason — otherwise noise.
      const iterations = typeof data.iterations === "number" ? data.iterations : 0
      const stop = typeof data.stop_reason === "string" ? data.stop_reason : ""
      const bits: string[] = []
      if (iterations > 1) bits.push(`${iterations} iterations`)
      if (stop && stop !== "success") bits.push(stop)
      if (bits.length === 0) return []
      return [{ kind: "system", label: "mooncake", text: bits.join(" · ") }]
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
