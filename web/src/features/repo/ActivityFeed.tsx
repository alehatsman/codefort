import type { ReactNode } from "react"
import { Link } from "react-router-dom"
import { timeAgo } from "@/shell/timeAgo"
import type { FleetEvent } from "@/api/types"

// ActivityFeed renders the live fleet event ring buffer from useFleetEvents.
// Each event is one row: a type badge, a human description, and a relative
// timestamp. Unknown event types fall back to a generic description.
export default function ActivityFeed({ events }: { events: FleetEvent[] }) {
  if (events.length === 0) {
    return <p className="activity-feed activity-feed--empty muted small">Waiting for activity…</p>
  }
  return (
    <ol className="activity-feed">
      {events.map((ev) => (
        <li key={ev.seq} className="activity-feed__item">
          <span className={`activity-feed__badge activity-feed__badge--${badgeClass(ev.type)}`}>
            {shortType(ev.type)}
          </span>
          <span className="activity-feed__desc">{describe(ev)}</span>
          <span
            className="activity-feed__time muted small"
            title={new Date(ev.time).toLocaleString()}
          >
            {timeAgo(new Date(ev.time).toISOString())}
          </span>
        </li>
      ))}
    </ol>
  )
}

// Abbreviate the event type for the badge: "issue.claimed" → "issue",
// "ci.run.queued" → "ci", "push" → "push".
function shortType(type: string): string {
  return type.split(".")[0]
}

// Map type to a CSS modifier for badge colour.
function badgeClass(type: string): string {
  if (type.startsWith("issue")) return "issue"
  if (type.startsWith("ci")) return "ci"
  if (type === "push") return "push"
  if (type.startsWith("pull") || type.startsWith("pr")) return "pull"
  return "default"
}

// Build a human-readable sentence for each event type.
function describe(ev: FleetEvent): ReactNode {
  const { type, repo, actor, data } = ev
  const repoLink = repo ? (
    <>
      {" in "}
      <Link className="activity-feed__repo" to={`/${repo}`}>
        {repo}
      </Link>
    </>
  ) : null
  const num = typeof data?.number === "number" ? data.number : null

  switch (type) {
    case "push":
      return (
        <>
          {actor && <strong>{actor}</strong>} pushed{" "}
          {data?.ref ? <code>{String(data.ref).replace("refs/heads/", "")}</code> : null}
          {repoLink}
        </>
      )
    case "issue.created":
      return (
        <>
          issue {num !== null ? `#${num}` : ""} created{repoLink}
        </>
      )
    case "issue.claimed":
      return (
        <>
          issue {num !== null ? `#${num}` : ""} claimed
          {actor ? (
            <>
              {" by "}
              <strong>{actor}</strong>
            </>
          ) : null}
          {repoLink}
        </>
      )
    case "issue.unclaimed":
      return (
        <>
          issue {num !== null ? `#${num}` : ""} unclaimed{repoLink}
        </>
      )
    case "issue.state_changed": {
      const state = typeof data?.state === "string" ? data.state : null
      return (
        <>
          issue {num !== null ? `#${num}` : ""} → {state ?? "?"}
          {repoLink}
        </>
      )
    }
    case "issue.updated":
    case "issue.commented":
      return (
        <>
          {type === "issue.commented" ? "comment on" : "updated"} issue{" "}
          {num !== null ? `#${num}` : ""}
          {repoLink}
        </>
      )
    case "ci.run.queued":
      return (
        <>
          CI run {num !== null ? `#${num}` : ""} queued{repoLink}
        </>
      )
    case "repo.deleted":
      return <>{actor && <strong>{actor}</strong>} deleted a repo</>
    default:
      return (
        <>
          {type.replace(/\./g, " ")}
          {repo ? ` · ${repo}` : ""}
        </>
      )
  }
}
