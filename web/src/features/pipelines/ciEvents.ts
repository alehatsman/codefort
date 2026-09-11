import { useEffect, useRef, useState } from "react"
import { getToken } from "@/api/client"
import type { CIEvent } from "@/api/types"

// CI job event-stream consumer.
//
// The events endpoint is Server-Sent Events on the Bearer-gated /api surface.
// The browser's native EventSource can't set an Authorization header, so we
// consume the stream with fetch + a ReadableStream reader instead, parsing the
// SSE framing by hand. Functionally identical (replay from the start, then
// live-tail), but it carries the token.
//
// The server closes the stream once the run is terminal; while the run is still
// live we reconnect on an unexpected drop, resuming from the last seq via the
// Last-Event-ID header so we don't replay what we already have.

interface JobEvents {
  events: CIEvent[]
  // done is true once the server closed the stream cleanly (run terminal).
  done: boolean
  error: string | null
}

export function useJobEventStream(
  owner: string,
  repo: string,
  runNumber: number,
  job: string,
  enabled: boolean,
  // Bumping resubscribeKey (same run+job) re-opens a stream the server closed at
  // a resting point — an agent run parks at awaiting_input between turns, where
  // the server ends the response (so it flushes through buffering proxies), and
  // the next turn needs a fresh connection. A resume, not a reset: we keep the
  // rendered transcript and pick up from the last seq.
  resubscribeKey = 0
): JobEvents {
  const [events, setEvents] = useState<CIEvent[]>([])
  const [done, setDone] = useState(false)
  const [error, setError] = useState<string | null>(null)
  // Highest seq seen, for resume across reconnects. A ref so the reconnect
  // loop reads the latest value without re-subscribing.
  const lastSeq = useRef(0)
  // Identity of the current subscription. A resubscribeKey bump on the same
  // identity resumes; any identity change is a fresh subscription (reset).
  const identity = useRef("")

  // biome-ignore lint/correctness/useExhaustiveDependencies: resubscribeKey isn't read in the effect body — bumping it is the intentional signal to tear down the stream and re-open it for the next turn.
  useEffect(() => {
    if (!enabled || !owner || !repo || !job || !Number.isFinite(runNumber)) return

    const id = `${owner}/${repo}/${runNumber}/${job}`
    if (identity.current !== id) {
      identity.current = id
      lastSeq.current = 0
      setEvents([])
    }

    const ctrl = new AbortController()
    let cancelled = false
    setDone(false)
    setError(null)

    const url = `/api/repos/${owner}/${repo}/runs/${runNumber}/jobs/${encodeURIComponent(
      job
    )}/events`

    void runStream(url, ctrl, lastSeq, () => cancelled, {
      onEvent: (ev) => {
        lastSeq.current = Math.max(lastSeq.current, ev.seq)
        setEvents((prev) => [...prev, ev])
      },
      onError: setError,
      onDone: () => setDone(true),
    })
    return () => {
      cancelled = true
      ctrl.abort()
    }
  }, [owner, repo, runNumber, job, enabled, resubscribeKey])

  return { events, done, error }
}

interface StreamHandlers {
  onEvent: (ev: CIEvent) => void
  onError: (msg: string) => void
}

// runStream owns the reconnect loop: fetch + consume, then retry on a
// transient drop while the run is still live, until the stream reports a
// clean close (done) or an HTTP-level error. Top-level (not nested inside the
// effect that starts it) so its own control flow doesn't stack cognitive
// complexity on top of the effect's.
async function runStream(
  url: string,
  ctrl: AbortController,
  lastSeq: { current: number },
  isCancelled: () => boolean,
  handlers: StreamHandlers & { onDone: () => void }
) {
  // Reconnect while the run is live and we haven't been told the stream is
  // done; a clean close (done) breaks the loop.
  while (!isCancelled()) {
    try {
      const outcome = await streamOnce(url, lastSeq.current, ctrl.signal, handlers)
      if (outcome === "error") return
      // A paused close means the run is still live and the server just
      // rotated the connection so a buffering hop flushes — reconnect from
      // lastSeq (the `while` condition above re-checks isCancelled on the
      // next spin, so a cancel during this stream exits there). A closed
      // stream with no resume sentinel means the run (or this job) is
      // terminal — mark done and stop reconnecting.
      if (outcome === "paused") continue
      if (isCancelled()) return
      handlers.onDone()
      return
    } catch {
      if (isCancelled() || ctrl.signal.aborted) return
      // Transient network drop on a live run — back off and resume.
      await sleep(1000)
    }
  }
}

// streamOnce makes one fetch + SSE-consume attempt (the caller's `run` loop
// retries on a transient drop). Returns "error" on an HTTP-level failure
// (already reported via onError), or consumeSSE's "paused"/"closed".
async function streamOnce(
  url: string,
  lastSeq: number,
  signal: AbortSignal,
  handlers: StreamHandlers
): Promise<"paused" | "closed" | "error"> {
  const headers = new Headers({ Accept: "text/event-stream" })
  const token = getToken()
  if (token) headers.set("Authorization", `Bearer ${token}`)
  if (lastSeq > 0) headers.set("Last-Event-ID", String(lastSeq))

  const resp = await fetch(url, { headers, signal })
  if (!resp.ok || !resp.body) {
    handlers.onError(`stream failed (${resp.status})`)
    return "error"
  }
  return consumeSSE(resp.body, handlers.onEvent)
}

// consumeSSE reads one SSE stream to its natural end (a network drop throws
// out of `reader.read()`, which the caller's try/catch handles), calling
// onEvent for each parsed frame. Returns "paused" for the server's resume
// sentinel (still-live run, reconnect from lastSeq) or "closed" for a clean
// end (run/job terminal).
async function consumeSSE(
  body: ReadableStream<Uint8Array>,
  onEvent: (ev: CIEvent) => void
): Promise<"paused" | "closed"> {
  const reader = body.getReader()
  const decoder = new TextDecoder()
  let buf = ""
  let paused = false
  for (;;) {
    const { value, done: streamDone } = await reader.read()
    if (streamDone) break
    buf += decoder.decode(value, { stream: true })
    // SSE frames are separated by a blank line.
    let sep = buf.indexOf("\n\n")
    while (sep !== -1) {
      const frame = buf.slice(0, sep)
      buf = buf.slice(sep + 2)
      if (isResumeFrame(frame)) {
        paused = true
      } else {
        const ev = parseFrame(frame)
        if (ev) onEvent(ev)
      }
      sep = buf.indexOf("\n\n")
    }
  }
  return paused ? "paused" : "closed"
}

// parseFrame extracts the CIEvent from one SSE frame. The event's full JSON is
// the `data:` payload (the id:/event: fields are redundant SSE framing); a
// comment frame (": ping" heartbeat) carries no data and is ignored.
function parseFrame(frame: string): CIEvent | null {
  for (const line of frame.split("\n")) {
    if (line.startsWith("data:")) {
      const json = line.slice(5).trim()
      if (!json) return null
      try {
        return JSON.parse(json) as CIEvent
      } catch {
        return null
      }
    }
  }
  return null
}

// isResumeFrame detects the server's non-terminal close sentinel — an
// `event: resume` frame carrying no data — emitted when a still-live stream is
// rotated to flush a buffering hop. It's never a real event (those always
// carry a data line), so parseFrame ignores it; we act on it separately.
function isResumeFrame(frame: string): boolean {
  return frame.split("\n").some((line) => line === "event: resume")
}

function sleep(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms))
}
