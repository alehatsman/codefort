import { useEffect, useRef, useState } from "react"
import { getToken } from "@/api/client"
import type { FleetEvent } from "@/api/types"

const RING_SIZE = 30

// useFleetEvents subscribes to the fleet-wide SSE event feed (GET /api/events)
// and maintains a ring buffer of the last RING_SIZE events, newest first.
// The connection lives for the lifetime of the component — it reconnects
// automatically on transient drops. Live-only: no initial replay (the feed
// populates as activity occurs).
export function useFleetEvents(): FleetEvent[] {
  const [events, setEvents] = useState<FleetEvent[]>([])
  const lastSeq = useRef(0)

  useEffect(() => {
    const ctrl = new AbortController()
    let cancelled = false

    async function run() {
      while (!cancelled) {
        try {
          const headers = new Headers({ Accept: "text/event-stream" })
          const token = getToken()
          if (token) headers.set("Authorization", `Bearer ${token}`)
          // Resume from last seen seq so reconnects don't re-deliver what we
          // already have. On first connect lastSeq=0, so the server streams
          // from now (WHERE seq > 0 picks up the full backlog — acceptable for
          // a personal fleet with a bounded event table).
          if (lastSeq.current > 0) headers.set("Last-Event-ID", String(lastSeq.current))

          const resp = await fetch("/api/events", { headers, signal: ctrl.signal })
          if (!resp.ok || !resp.body) {
            await sleep(5000)
            continue
          }

          const reader = resp.body.getReader()
          const decoder = new TextDecoder()
          let buf = ""
          for (;;) {
            const { value, done } = await reader.read()
            if (done) break
            buf += decoder.decode(value, { stream: true })
            let sep = buf.indexOf("\n\n")
            while (sep !== -1) {
              const frame = buf.slice(0, sep)
              buf = buf.slice(sep + 2)
              const ev = parseFrame(frame)
              if (ev) {
                lastSeq.current = Math.max(lastSeq.current, ev.seq)
                setEvents((prev) => [ev, ...prev].slice(0, RING_SIZE))
              }
              sep = buf.indexOf("\n\n")
            }
          }
          // Server closed the stream — reconnect after a brief pause.
          if (!cancelled) await sleep(1000)
        } catch {
          if (cancelled || ctrl.signal.aborted) return
          await sleep(3000)
        }
      }
    }

    run()
    return () => {
      cancelled = true
      ctrl.abort()
    }
  }, [])

  return events
}

function parseFrame(frame: string): FleetEvent | null {
  for (const line of frame.split("\n")) {
    if (line.startsWith("data:")) {
      const json = line.slice(5).trim()
      if (!json) return null
      try {
        return JSON.parse(json) as FleetEvent
      } catch {
        return null
      }
    }
  }
  return null
}

function sleep(ms: number): Promise<void> {
  return new Promise((r) => setTimeout(r, ms))
}
