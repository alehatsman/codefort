import { useEffect, useRef, useState } from "react"
import { Link } from "react-router-dom"
import type { FleetEvent } from "@/api/types"
import { useFleetEvents } from "@/features/repo/useFleetEvents"

const LS_KEY = "notif-last-seen"

function getLastSeen(): number {
  return Number(localStorage.getItem(LS_KEY) ?? "0")
}

function setLastSeen(seq: number) {
  localStorage.setItem(LS_KEY, String(seq))
}

function eventLabel(ev: FleetEvent): string {
  const t = ev.type
  if (t === "push") return `${ev.actor ?? "someone"} pushed to ${ev.repo ?? "a repo"}`
  if (t.startsWith("issue")) return `Issue ${t.replace("issue.", "")} in ${ev.repo ?? "a repo"}`
  if (t.startsWith("ci") || t.startsWith("run"))
    return `CI ${t.replace(/^(ci|run)\./, "")} in ${ev.repo ?? "a repo"}`
  if (t.startsWith("agent")) return `Agent ${t.replace("agent.", "")} in ${ev.repo ?? "a repo"}`
  if (t.startsWith("pr") || t.startsWith("pull"))
    return `PR ${t.replace(/^(pr|pull)\./, "")} in ${ev.repo ?? "a repo"}`
  return `${t} in ${ev.repo ?? "a repo"}`
}

function eventHref(ev: FleetEvent): string {
  if (!ev.repo) return "/"
  const base = `/${ev.repo}`
  const n =
    (ev.data?.["number"] as number | undefined) ?? (ev.data?.["run_number"] as number | undefined)
  if (ev.type === "push") return `${base}/commits`
  if (ev.type.startsWith("issue") && n) return `${base}/issues/${n}`
  if ((ev.type.startsWith("ci") || ev.type.startsWith("run")) && n) return `${base}/pipelines/${n}`
  if (ev.type.startsWith("agent") && n) return `${base}/agents/${n}`
  if ((ev.type.startsWith("pr") || ev.type.startsWith("pull")) && n) return `${base}/pulls/${n}`
  return base
}

export default function NotificationBell() {
  const events = useFleetEvents()
  const [lastSeen, setLastSeenState] = useState(getLastSeen)
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  const unread = events.filter((e) => e.seq > lastSeen).length

  function markRead() {
    if (events.length === 0) return
    const max = Math.max(...events.map((e) => e.seq))
    setLastSeen(max)
    setLastSeenState(max)
  }

  function toggle() {
    if (!open) markRead()
    setOpen((v) => !v)
  }

  // Close on outside click
  useEffect(() => {
    if (!open) return
    function handler(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener("mousedown", handler)
    return () => document.removeEventListener("mousedown", handler)
  }, [open])

  return (
    <div className="notif-bell" ref={ref}>
      <button type="button" className="notif-bell__btn" onClick={toggle} aria-label="Notifications">
        <svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true">
          <path d="M8 1a5 5 0 0 0-5 5v3l-1 1.5V12h12v-1.5L13 9V6a5 5 0 0 0-5-5Zm0 13.5a1.5 1.5 0 0 0 1.5-1.5h-3A1.5 1.5 0 0 0 8 14.5Z" />
        </svg>
        {unread > 0 && <span className="notif-bell__badge">{unread > 9 ? "9+" : unread}</span>}
      </button>
      {open && (
        <div className="notif-dropdown">
          {events.length === 0 ? (
            <div className="notif-dropdown__empty muted small">No recent activity</div>
          ) : (
            <ul className="notif-list">
              {events.slice(0, 15).map((ev) => (
                <li
                  key={ev.seq}
                  className={`notif-row${ev.seq > lastSeen ? " notif-row--unread" : ""}`}
                >
                  <Link
                    to={eventHref(ev)}
                    className="notif-row__link"
                    onClick={() => setOpen(false)}
                  >
                    <span className="notif-row__label">{eventLabel(ev)}</span>
                    {ev.actor && <span className="notif-row__actor muted small">{ev.actor}</span>}
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  )
}
