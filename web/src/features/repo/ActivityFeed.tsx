import clsx from "clsx"
import type { ReactNode } from "react"
import { Link } from "react-router-dom"
import type { FleetEvent } from "@/api/types"
import { timeAgo } from "@/shell/timeAgo"

// ActivityFeed renders the live fleet event ring buffer from useFleetEvents.
// Each event is one row: a type badge, a human description that links to the
// entity it concerns (issue / PR / run / commit), the owning repo, and a
// relative timestamp. Unknown event types fall back to a generic description.
export default function ActivityFeed({ events }: { events: FleetEvent[] }) {
  if (events.length === 0) {
    return <p className="activity-feed activity-feed--empty muted small">Waiting for activity…</p>
  }
  return (
    <ol className="activity-feed">
      {events.map((ev) => {
        const p = parse(ev)
        const main = (
          <>
            <span className={clsx("activity-feed__badge", `activity-feed__badge--${p.tone}`)}>
              {p.badge}
            </span>
            <span className="activity-feed__desc">{p.body}</span>
          </>
        )
        return (
          <li key={ev.seq} className="activity-feed__item">
            {p.href ? (
              <Link className="activity-feed__main" to={p.href}>
                {main}
              </Link>
            ) : (
              <span className="activity-feed__main">{main}</span>
            )}
            {ev.repo ? (
              <Link className="activity-feed__repo" to={`/${ev.repo}`}>
                {ev.repo}
              </Link>
            ) : null}
            <span
              className="activity-feed__time muted small"
              title={new Date(ev.time).toLocaleString()}
            >
              {timeAgo(new Date(ev.time).toISOString())}
            </span>
          </li>
        )
      })}
    </ol>
  )
}

interface Parsed {
  // Route to the entity this event concerns, or null when there's nothing to
  // open (e.g. a deleted repo). The whole description becomes a link when set.
  href: string | null
  // Short badge label + colour tone modifier.
  badge: string
  tone: string
  // Human description, sans repo (the repo is rendered as a separate chip).
  body: ReactNode
}

// parse turns one fleet event into its badge, link target, and description.
function parse(ev: FleetEvent): Parsed {
  const { type, actor, data } = ev
  const repo = ev.repo ?? ""
  const num = typeof data?.["number"] === "number" ? data["number"] : null
  // Actor prefix carries its own trailing space so call sites read `{who}verb`.
  const who = actor ? (
    <>
      <strong>{actor}</strong>{" "}
    </>
  ) : null

  switch (type) {
    case "push":
      return pushRow(repo, data, who)
    case "issue.created":
      return issueRow(repo, num, "created", title(data), who)
    case "issue.claimed":
      return issueRow(repo, num, "claimed", null, who)
    case "issue.unclaimed":
      return issueRow(repo, num, "unclaimed", null, who)
    case "issue.commented":
      return issueRow(repo, num, "commented on", null, who)
    case "issue.updated":
      return issueRow(repo, num, "updated", null, who)
    case "issue.state_changed":
      return issueStateChangedRow(repo, num, data)
    case "pull.opened":
      return pullRow(repo, num, "opened", title(data), who, "pull")
    case "pull.merged":
      return pullRow(repo, num, "merged", null, who, "ok")
    case "pull.closed":
      return pullRow(repo, num, "closed", null, who, "muted")
    case "review.submitted":
      return reviewSubmittedRow(repo, num, data, who)
    case "ci.run.queued":
      return runRow(repo, num, "pipelines", "CI", "queued", "ci", who)
    case "ci.run.finished":
      return ciRunFinishedRow(repo, num, data)
    case "agent.run.started":
      return runRow(repo, num, "agents", "agent", "started", "agent", who)
    case "agent.run.awaiting_input":
      return {
        href: runHref(repo, num, "agents"),
        badge: "agent",
        tone: "warn",
        body: <>agent {numLabel(num)} awaiting input</>,
      }
    case "agent.run.finished":
      return agentRunFinishedRow(repo, num, data)
    case "repo.deleted":
      return { href: null, badge: "repo", tone: "default", body: <>{who}deleted a repo</> }
    default:
      return defaultRow(type, repo)
  }
}

function pushRow(repo: string, data: FleetEvent["data"], who: ReactNode): Parsed {
  const branch = data?.["ref"] ? String(data["ref"]).replace("refs/heads/", "") : null
  const sha = typeof data?.["after"] === "string" ? data["after"].slice(0, 7) : null
  return {
    href: repo && sha ? `/${repo}/commit/${data?.["after"]}` : repo ? `/${repo}` : null,
    badge: "push",
    tone: "push",
    body: (
      <>
        {who}pushed {branch ? <code>{branch}</code> : null}
        {sha ? <code className="activity-feed__sha">{sha}</code> : null}
      </>
    ),
  }
}

function issueStateChangedRow(repo: string, num: number | null, data: FleetEvent["data"]): Parsed {
  const state = typeof data?.["state"] === "string" ? data["state"] : "?"
  return {
    href: issueHref(repo, num),
    badge: "issue",
    tone: "issue",
    body: (
      <>
        issue {numLabel(num)} → <code>{state}</code>
      </>
    ),
  }
}

function reviewSubmittedRow(
  repo: string,
  num: number | null,
  data: FleetEvent["data"],
  who: ReactNode
): Parsed {
  const state = typeof data?.["state"] === "string" ? data["state"] : null
  return {
    href: pullHref(repo, num),
    badge: "review",
    tone: state === "approved" ? "ok" : state === "changes_requested" ? "fail" : "review",
    body: (
      <>
        {who}reviewed PR {numLabel(num)}
        {state ? <> · {state.replace(/_/g, " ")}</> : null}
      </>
    ),
  }
}

function ciRunFinishedRow(repo: string, num: number | null, data: FleetEvent["data"]): Parsed {
  const status = typeof data?.["status"] === "string" ? data["status"] : "finished"
  return {
    href: runHref(repo, num, "pipelines"),
    badge: "ci",
    tone: runTone(status),
    body: (
      <>
        CI {numLabel(num)} <code>{status}</code>
      </>
    ),
  }
}

function agentRunFinishedRow(repo: string, num: number | null, data: FleetEvent["data"]): Parsed {
  const status = typeof data?.["status"] === "string" ? data["status"] : "finished"
  return {
    href: runHref(repo, num, "agents"),
    badge: "agent",
    tone: runTone(status),
    body: (
      <>
        agent {numLabel(num)} <code>{status}</code>
      </>
    ),
  }
}

function defaultRow(type: string, repo: string): Parsed {
  return {
    href: repo ? `/${repo}` : null,
    badge: type.split(".")[0] ?? type,
    tone: "default",
    body: <>{type.replace(/\./g, " ")}</>,
  }
}

// title pulls a human title from an event payload, when present.
function title(data: FleetEvent["data"]): string | null {
  return typeof data?.["title"] === "string" && data["title"] ? data["title"] : null
}

function numLabel(num: number | null): string {
  return num !== null ? `#${num}` : ""
}

function issueHref(repo: string, num: number | null): string | null {
  return repo && num !== null ? `/${repo}/issues/${num}` : repo ? `/${repo}` : null
}

function pullHref(repo: string, num: number | null): string | null {
  return repo && num !== null ? `/${repo}/pulls/${num}` : repo ? `/${repo}` : null
}

function runHref(repo: string, num: number | null, seg: "pipelines" | "agents"): string | null {
  return repo && num !== null ? `/${repo}/${seg}/${num}` : repo ? `/${repo}` : null
}

// runTone maps a terminal run/CI status to a badge colour tone.
function runTone(status: string): string {
  if (status === "success" || status === "passed") return "ok"
  if (status === "failed" || status === "error" || status === "canceled") return "fail"
  return "ci"
}

function issueRow(
  repo: string,
  num: number | null,
  verb: string,
  t: string | null,
  who: ReactNode
): Parsed {
  return {
    href: issueHref(repo, num),
    badge: "issue",
    tone: "issue",
    body: (
      <>
        {who}
        {verb} issue {numLabel(num)}
        {t ? <span className="activity-feed__title">{t}</span> : null}
      </>
    ),
  }
}

function pullRow(
  repo: string,
  num: number | null,
  verb: string,
  t: string | null,
  who: ReactNode,
  tone: string
): Parsed {
  return {
    href: pullHref(repo, num),
    badge: "pull",
    tone,
    body: (
      <>
        {who}
        {verb} PR {numLabel(num)}
        {t ? <span className="activity-feed__title">{t}</span> : null}
      </>
    ),
  }
}

function runRow(
  repo: string,
  num: number | null,
  seg: "pipelines" | "agents",
  label: string,
  verb: string,
  tone: string,
  who: ReactNode
): Parsed {
  return {
    href: runHref(repo, num, seg),
    badge: seg === "agents" ? "agent" : "ci",
    tone,
    body: (
      <>
        {who}
        {label} {numLabel(num)} {verb}
      </>
    ),
  }
}
