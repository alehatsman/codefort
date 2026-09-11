import { useVirtualizer } from "@tanstack/react-virtual"
import "./agents.css"
import clsx from "clsx"
import { Fragment, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react"
import { useCancelAgentRun, useCreateAgentTurn, useFinishAgentRun } from "@/api/mutations"
import type { CIEvent, CIRunDetail } from "@/api/types"
// Temporary seam: agents reuse the pipelines run-event stream + duration
// helper. The run shell is shared today (one PipelinesPage/GlobalRunsPage takes
// a `kind` prop); when agents grow their own run components this dependency on
// pipelines/ should be cut over to agents-local equivalents.
import { useJobEventStream } from "@/features/pipelines/ciEvents"
import { formatDuration } from "@/features/pipelines/runHelpers"
import { Button, ErrorMessage, Spinner } from "@/ui"

// Terminal agent-run statuses: no further turns, the message box closes.
const TERMINAL = ["success", "failed", "canceled", "error", "interrupted", "stalled"]

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
  // Append a card per pending follow-up turn — a message the human sent that
  // the agent hasn't picked up yet. A pending turn isn't in the event stream
  // (it only enters as a "Turn N" card once it goes running), so without this
  // the sent message is invisible between send and pickup. We render only
  // pending turns, so there's no overlap with the transcript's turn cards.
  const cards = useMemo(() => {
    const built = buildCards(entries)
    for (const t of (run.turns ?? []).filter((turn) => turn.status === "pending")) {
      built.push({
        id: `queued-${t.seq}`,
        title: "You · queued",
        queued: true,
        body: [{ id: `queued-${t.seq}-body`, kind: "assistant", text: t.body }],
      })
    }
    return built
  }, [entries, run.turns])

  // A turn is in flight while its agent.turn.started has no matching
  // turn.completed. During that window the agent is busy but emits nothing
  // while it plans (the now-removed mooncake-agent model ran Claude as a
  // one-shot planner, then bursted the steps — old runs replay that same
  // pattern), so without a hint the silent gap reads as a hang.
  // "planning" until the turn produces its first entry, "working" once
  // steps/output begin. Gated on the run's terminal status (not the stream's
  // `done`, which also closes on error) so a parked awaiting_input run — turn
  // already completed — correctly shows nothing.
  const terminal = TERMINAL.includes(run.status)
  const phase = useMemo<"planning" | "working" | null>(
    () => computeAgentPhase(terminal, error, events, entries),
    [events, entries, terminal, error]
  )

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
    getItemKey: (i) => cards[i]?.id ?? i,
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
      <ErrorMessage error={error} inline />
      {cards.length === 0 && !done && <Spinner label="Waiting for the agent…" />}
      <div ref={scrollRef} className="agent-transcript" onScroll={onScroll}>
        <div className="agent-transcript__sizer" style={{ height: total }}>
          {virtualizer.getVirtualItems().map((vi) => {
            const card = cards[vi.index]
            if (!card) return null
            return (
              <div
                key={vi.key}
                data-index={vi.index}
                ref={virtualizer.measureElement}
                className="agent-transcript__row"
                style={{ transform: `translateY(${vi.start}px)` }}
              >
                {renderCard(card)}
              </div>
            )
          })}
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
  // queued marks a human follow-up message that's been sent but not yet picked
  // up by the agent (a pending turn). It renders with a distinct pending tint
  // so the message stays visible in the log instead of vanishing until the
  // agent's turn card appears.
  queued?: boolean
  body: AgentEntry[]
}

// computeAgentPhase derives "planning"/"working"/null for the in-flight-turn
// hint. Pulled out to module scope so its branches don't stack cognitive
// complexity on top of the useMemo that calls it.
function computeAgentPhase(
  terminal: boolean,
  error: string | null,
  events: CIEvent[],
  entries: AgentEntry[]
): "planning" | "working" | null {
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
}

// buildCards groups the flat entries into cards. A "turn" or a "plan" system
// entry opens a card (its label becomes the title); every following non-header
// entry is appended to that card's body. A turn's prompt rides the turn entry's
// text, so it's pushed into the body as prose.
function buildCards(entries: AgentEntry[]): Card[] {
  const cards = groupEntriesIntoCards(entries)
  for (const c of cards) deriveCardStatus(c)
  return cards
}

function isCardHeader(e: AgentEntry): boolean {
  return e.kind === "turn" || (e.kind === "system" && (e.label ?? "").startsWith("plan"))
}

function groupEntriesIntoCards(entries: AgentEntry[]): Card[] {
  const cards: Card[] = []
  let cur: Card | null = null
  for (const e of entries) {
    if (isCardHeader(e)) {
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
  return cards
}

// Derive a card's head status from its steps: failed wins, then running,
// else ok if it ran any step at all.
function deriveCardStatus(c: Card): void {
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

// renderCard draws one section as a CI-style step card: a head (status dot +
// title) over a body of rendered entries.
function renderCard(card: Card) {
  return (
    <div className={clsx("ci-step agent-card", { "agent-card--queued": card.queued })}>
      <div className={clsx("ci-step__head", { "ci-step__head--failed": card.status === "failed" })}>
        {card.queued ? (
          <span className="agent-card__queued-glyph" aria-hidden="true">
            ◷
          </span>
        ) : (
          card.status && <span className={`ci-step__status ci-step__status--${card.status}`} />
        )}
        <span className="ci-step__cmd">{card.title}</span>
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

  if (terminal) {
    return (
      <div className="agent-msgbox agent-msgbox--done muted small">
        This agent run has finished
        {run.status === "success"
          ? " — see the issue for the result branch."
          : run.status === "stalled"
            ? " — the agent stopped without making progress; refine the issue and start a new run."
            : "."}
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
        <Button
          size="small"
          onClick={() => finish.mutate()}
          disabled={finish.isPending || run.status === "running"}
          title="Hand off the agent's work: push agent/issue-N and comment on the issue"
        >
          {finish.isPending ? "Finishing…" : "Finish"}
        </Button>
        <Button
          size="small"
          variant="danger"
          onClick={() => cancel.mutate()}
          disabled={cancel.isPending}
          title="Force-stop the run now: interrupt the agent and discard the workspace (no branch is handed off)"
        >
          {cancel.isPending ? "Stopping…" : "Stop"}
        </Button>
        <Button
          type="submit"
          size="small"
          variant="primary"
          disabled={send.isPending || text.trim() === ""}
        >
          {send.isPending ? "Sending…" : "Send"}
        </Button>
      </div>
      <ErrorMessage error={send.error} inline />
      <ErrorMessage error={finish.error} inline />
      <ErrorMessage error={cancel.error} inline />
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
  id?: string | undefined
  kind:
    | "step"
    | "turn"
    | "thinking"
    | "planning"
    | "assistant"
    | "tool_use"
    | "tool_result"
    | "result"
    | "system"
    | "raw"
  // status is set only on "step" entries; it's mutated in place when the step
  // resolves so the live row flips ▶ → ✓/~/✗ without spawning a second line.
  status?: StepStatus | undefined
  label?: string | undefined
  text: string
}

// foldAgentEvents reduces the agent event stream into display entries. An
// agent.message carries either a claude stream-json object under data.claude
// (claude-edit runs — the only kind any run can produce today) or a mooncake
// NDJSON event under data.mooncake (mooncake-agent runs — removed as a
// spawnable option (#110), but old runs' stored transcript events still carry
// this shape, so the fold stays to render their history correctly); we fold
// each into human-meaningful entries and fall back to compact JSON for
// anything unrecognized, so the transcript stays faithful even as either
// schema evolves.
function foldAgentEvents(events: CIEvent[]): AgentEntry[] {
  const out: AgentEntry[] = []
  // A historical mooncake-agent run streamed each step as separate
  // started/stdout/completed events; accumulate per-step output to emit one
  // entry per completed step.
  const mc: MooncakeState = { steps: new Map() }

  for (const ev of events) {
    const d = ev.data ?? {}
    // Tag every entry this event produces with a key stable across appends and
    // deterministic re-folds: the event seq plus its position within the event.
    const start = out.length
    out.push(...foldOneAgentEvent(ev.type, d, mc, out))
    for (let i = start; i < out.length; i++) {
      const entry = out[i]
      if (entry) entry.id = `${ev.seq}.${i - start}`
    }
  }
  return out
}

// foldOneAgentEvent dispatches one event to its case handler, pulled out to
// module scope so the switch doesn't stack cognitive complexity on top of
// the fold loop.
function foldOneAgentEvent(
  type: string,
  d: Record<string, unknown>,
  mc: MooncakeState,
  out: AgentEntry[]
): AgentEntry[] {
  switch (type) {
    case "agent.turn.started":
      return [foldTurnStarted(d)]
    case "agent.turn.completed":
      return [foldTurnCompleted(d)]
    case "agent.raw":
      return [{ kind: "raw", text: asString(d["line"]) }]
    case "agent.message":
      return d["mooncake"]
        ? foldMooncakeEvent(d["mooncake"] as Record<string, unknown>, mc, out)
        : foldClaudeMessage(d["claude"] as Record<string, unknown> | undefined)
    default:
      return []
  }
}

function foldTurnStarted(d: Record<string, unknown>): AgentEntry {
  const turn = typeof d["turn"] === "number" ? d["turn"] : "?"
  return { kind: "turn", label: `Turn ${turn}`, text: asString(d["prompt"]) }
}

function foldTurnCompleted(d: Record<string, unknown>): AgentEntry {
  const status = typeof d["status"] === "string" ? d["status"] : "done"
  const bits = [`status: ${status}`]
  if (typeof d["num_turns"] === "number" && d["num_turns"] > 0) bits.push(`${d["num_turns"]} steps`)
  if (typeof d["duration_ms"] === "number" && d["duration_ms"] > 0)
    bits.push(formatDuration(d["duration_ms"]))
  if (typeof d["cost_usd"] === "number" && d["cost_usd"] > 0)
    bits.push(`$${d["cost_usd"].toFixed(4)}`)
  return { kind: "result", label: "Turn complete", text: bits.join(" · ") }
}

// MooncakeState carries the in-flight mooncake steps across the fold: a step's
// output (stdout/stderr/file changes) streams between its started and completed
// events, so we buffer it keyed by step_id (last = the step in flight, for
// events like file.created that omit step_id).
interface MooncakeState {
  // index is the step's row position in the fold's `out` array, so a later
  // step.completed mutates the same row the step.started pushed (the live ▶
  // line flips in place rather than appending a second row).
  steps: Map<
    string,
    { action?: string | undefined; name?: string | undefined; lines: string[]; index: number }
  >
  last?: string | undefined
  // The in-flight planner entry during the plan phase (#171). mooncake #76
  // streams the planner's output as planner.delta between plan.generating and
  // plan.loaded; we accumulate a run of same-kind deltas into one row (mutated
  // in place) and start a new row when the kind flips (text ⇄ thinking).
  planner?: { index: number; kind: AgentEntry["kind"] } | undefined
}

// foldMooncakeEvent turns one mooncake NDJSON event (data.mooncake on a
// historical mooncake-agent run's agent.message — mooncake-agent can no
// longer be spawned, but old runs' stored events still carry this shape) into
// transcript entries, mirroring mooncake's own terminal renderer: each step
// is one line whose glyph flips
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
  const type = typeof m["type"] === "string" ? m["type"] : ""
  const data = (m["data"] ?? {}) as Record<string, unknown>

  switch (type) {
    case "plan.generating":
      // The "started" bracket for the plan phase (#76). Open a fresh planner
      // block; the deltas that follow stream into it. The turn header already
      // brackets this above, so no row of its own — just reset the accumulator
      // so a re-plan iteration starts a new run of rows.
      mc.planner = undefined
      return []
    case "planner.delta":
      return foldPlannerDelta(data, mc, out)
    case "plan.loaded":
      return foldPlanLoaded(data, mc)
    case "step.started":
      return foldStepStarted(data, mc, out)
    case "step.stdout":
    case "step.stderr":
      return foldStepOutput(data, mc)
    case "file.created":
    case "file.modified":
    case "file.deleted":
      return foldFileEvent(type, data, mc)
    case "step.completed":
    case "step.failed":
    case "step.skipped":
      return foldStepTerminal(type, data, mc, out)
    case "run.completed":
      return foldRunCompleted(data)
    case "agent.completed":
      return foldAgentCompleted(data)
    default:
      // run.started, etc — redundant with the above / the turn line.
      return []
  }
}

// Live planner output. kind is "text" (the plan being written) or "thinking"
// (reasoning, rendered dimmed). Append to the current row when the kind
// matches; otherwise open a new row so the dim/normal styling tracks the
// kind. The first row of the block carries the "planning…" label so the
// reader knows the agent is mid-plan.
function foldPlannerDelta(
  data: Record<string, unknown>,
  mc: MooncakeState,
  out: AgentEntry[]
): AgentEntry[] {
  const text = asString(data["text"])
  if (!text) return []
  const kind: AgentEntry["kind"] = data["kind"] === "thinking" ? "thinking" : "planning"
  if (mc.planner && mc.planner.kind === kind) {
    const row = out[mc.planner.index]
    if (row) row.text += text
    return []
  }
  const index = out.length
  const label = mc.planner ? undefined : "planning…"
  out.push({ kind, label, text })
  mc.planner = { index, kind }
  return []
}

// The plan is final; close the planner block so any later delta (a re-plan
// iteration) opens a fresh row rather than appending here.
function foldPlanLoaded(data: Record<string, unknown>, mc: MooncakeState): AgentEntry[] {
  mc.planner = undefined
  const n = data["total_steps"]
  return [{ kind: "system", label: typeof n === "number" ? `plan · ${n} steps` : "plan", text: "" }]
}

// Push the live row now (▶) and remember where it sits so step.completed
// can flip it in place.
function foldStepStarted(
  data: Record<string, unknown>,
  mc: MooncakeState,
  out: AgentEntry[]
): AgentEntry[] {
  const id = asString(data["step_id"])
  const name = typeof data["name"] === "string" ? data["name"] : ""
  const index = out.length
  out.push({ kind: "step", status: "running", label: name || id || "step", text: "" })
  mc.steps.set(id, {
    action: typeof data["action"] === "string" ? data["action"] : undefined,
    name: name || undefined,
    lines: [],
    index,
  })
  mc.last = id
  return []
}

function foldStepOutput(data: Record<string, unknown>, mc: MooncakeState): AgentEntry[] {
  const id = data["step_id"] ? asString(data["step_id"]) : mc.last
  const step = id ? mc.steps.get(id) : undefined
  if (step) step.lines.push(asString(data["line"]))
  return []
}

// file.* carries no step_id; attach to the step in flight.
function foldFileEvent(
  type: string,
  data: Record<string, unknown>,
  mc: MooncakeState
): AgentEntry[] {
  const step = mc.last ? mc.steps.get(mc.last) : undefined
  if (step) step.lines.push(`${type.slice("file.".length)} ${asString(data["path"])}`)
  return []
}

// Resolves the terminal status from the event type or the result payload.
function resolveStepStatus(type: string, result: Record<string, unknown>): StepStatus {
  if (type === "step.failed" || result["failed"] || result["status"] === "failed") return "failed"
  if (type === "step.skipped" || result["status"] === "skipped") return "skipped"
  if (result["status"] === "changed") return "changed"
  return "ok"
}

// Details surface only on failure — keep the success log a clean list.
function failureLines(
  action: string,
  result: Record<string, unknown>,
  data: Record<string, unknown>,
  tracked: { lines: string[] } | undefined
): string {
  const lines: string[] = []
  // For cmd/shell steps the executed command rides result.target (the
  // rendered argv); lead with it as a `$ …` line so a failure shows what
  // actually ran. Other actions put a path/package in target — skip it.
  const cmdline = action === "cmd" || action === "shell" ? asString(result["target"]) : ""
  if (cmdline) lines.push(`$ ${cmdline}`)
  lines.push(...(tracked?.lines ?? []))
  const err = asString(result["error"]) || asString(data["error_message"])
  if (err) lines.push(err)
  return lines.join("\n")
}

function foldStepTerminal(
  type: string,
  data: Record<string, unknown>,
  mc: MooncakeState,
  out: AgentEntry[]
): AgentEntry[] {
  const id = asString(data["step_id"])
  const tracked = mc.steps.get(id)
  mc.steps.delete(id)
  const result = (data["result"] ?? {}) as Record<string, unknown>
  const action = tracked?.action ?? (typeof data["action"] === "string" ? data["action"] : "")
  const status = resolveStepStatus(type, result)
  const row = tracked ? out[tracked.index] : undefined
  if (!row) return []
  row.status = status
  if (status === "failed") row.text = failureLines(action, result, data, tracked)
  return []
}

function foldRunCompleted(data: Record<string, unknown>): AgentEntry[] {
  const num = (k: string) => (typeof data[k] === "number" ? (data[k] as number) : 0)
  const bits = [`ok=${num("success_steps")}`, `changed=${num("changed_steps")}`]
  if (num("skipped_steps") > 0) bits.push(`skipped=${num("skipped_steps")}`)
  bits.push(`failed=${num("failed_steps")}`)
  if (num("duration_ms") > 0) bits.push(formatDuration(num("duration_ms")))
  return [{ kind: "result", label: "RECAP", text: bits.join("  ") }]
}

// The (now-removed) mooncake agent wrapped one or more plan/execute loops;
// status + stop_reason already ride the "Turn complete" line, but the
// iteration count is shown nowhere else. Surface it only when it actually
// re-planned (>1) or stopped for a non-success reason — otherwise noise.
function foldAgentCompleted(data: Record<string, unknown>): AgentEntry[] {
  const iterations = typeof data["iterations"] === "number" ? data["iterations"] : 0
  const stop = typeof data["stop_reason"] === "string" ? data["stop_reason"] : ""
  const bits: string[] = []
  if (iterations > 1) bits.push(`${iterations} iterations`)
  if (stop && stop !== "success") bits.push(stop)
  if (bits.length === 0) return []
  return [{ kind: "system", label: "mooncake", text: bits.join(" · ") }]
}

// foldClaudeMessage turns one claude stream-json object into zero or more
// transcript entries.
function foldClaudeMessage(obj: Record<string, unknown> | undefined): AgentEntry[] {
  if (!obj || typeof obj["type"] !== "string") return []
  switch (obj["type"]) {
    case "system":
      return [{ kind: "system", label: "session", text: asString(obj["model"] ?? obj["subtype"]) }]
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
  const message = obj["message"] as { content?: unknown } | undefined
  const content = message?.content
  if (!Array.isArray(content)) return []
  const out: AgentEntry[] = []
  for (const block of content) {
    if (!block || typeof block !== "object") continue
    const b = block as Record<string, unknown>
    switch (b["type"]) {
      case "text":
        out.push({ kind: "assistant", text: asString(b["text"]) })
        break
      case "thinking":
        out.push({ kind: "thinking", label: "thinking", text: asString(b["thinking"]) })
        break
      case "tool_use":
        // Compact, like a terminal action line — the name is the signal; the
        // input args are dropped to keep the log scannable (details-on-failure).
        out.push({ kind: "tool_use", label: `🔧 ${asString(b["name"])}`, text: "" })
        break
      case "tool_result":
        out.push({ kind: "tool_result", label: "result", text: asString(b["content"]) })
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
