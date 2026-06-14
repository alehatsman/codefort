import clsx from "clsx"
import { Link } from "react-router-dom"
import { useAllIssues, useAllPulls, useAllRuns } from "@/api/queries"
import type { CIRunWithRepo, IssueWithRepo, PullRequestWithRepo, RepoRef } from "@/api/types"

// AttentionQueue is the Overview triage panel: a short, clickable list of the
// things an operator running a fleet of agents needs to act on, most-urgent
// first — agents parked awaiting input, failing CI, open PRs, and unclaimed
// work. Every row links straight to the entity. Sections with nothing in them
// collapse; when all are empty the panel reports "all clear".
//
// All data comes from existing fleet-wide queries (no backend change), so the
// panel reflects polled state rather than the live event stream.

const MAX_ROWS = 6

interface Row {
  key: string
  href: string
  repo: string
  num: number
  label: string
  // Secondary context (branch, status, …); empty string hides it.
  meta: string
}

interface Section {
  title: string
  tone: string
  rows: Row[]
}

export default function AttentionQueue() {
  const awaiting = useAllRuns("agent", "state=awaiting_input")
  const failedCI = useAllRuns("ci", "state=failed,error")
  const openPulls = useAllPulls("open")
  const todo = useAllIssues("state=todo")

  const sections: Section[] = [
    { title: "Agents awaiting input", tone: "warn", rows: (awaiting.data ?? []).map(agentRow) },
    { title: "Failing CI", tone: "fail", rows: (failedCI.data ?? []).map(ciRow) },
    { title: "Open PRs", tone: "pull", rows: (openPulls.data ?? []).map(pullRow) },
    {
      title: "Unclaimed work",
      tone: "issue",
      rows: (todo.data ?? []).filter((i) => !i.assignee).map(issueRow),
    },
  ].filter((s) => s.rows.length > 0)

  if (sections.length === 0) {
    const loading =
      awaiting.isLoading || failedCI.isLoading || openPulls.isLoading || todo.isLoading
    if (loading) return null
    return (
      <p className="attention attention--clear muted small">All clear — nothing needs attention.</p>
    )
  }

  return (
    <div className="attention">
      {sections.map((s) => (
        <section key={s.title} className={clsx("attention__group", `attention__group--${s.tone}`)}>
          <h3 className="attention__heading">
            {s.title}
            <span className="attention__count">{s.rows.length}</span>
          </h3>
          <ol className="attention__list">
            {s.rows.slice(0, MAX_ROWS).map((row) => (
              <li key={row.key}>
                <Link className="attention__row" to={row.href}>
                  <span className="attention__repo">{row.repo}</span>
                  <span className="attention__num">#{row.num}</span>
                  <span className="attention__label">{row.label}</span>
                  {row.meta ? (
                    <span className="attention__meta muted small">{row.meta}</span>
                  ) : null}
                </Link>
              </li>
            ))}
            {s.rows.length > MAX_ROWS ? (
              <li className="attention__more muted small">+{s.rows.length - MAX_ROWS} more</li>
            ) : null}
          </ol>
        </section>
      ))}
    </div>
  )
}

function slug(repo: RepoRef): string {
  return `${repo.owner}/${repo.name}`
}

function agentRow(r: CIRunWithRepo): Row {
  const repo = slug(r.repo)
  return {
    key: `agent:${repo}:${r.number}`,
    href: `/${repo}/agents/${r.number}`,
    repo,
    num: r.number,
    label: r.issue_number ? `working issue #${r.issue_number}` : (r.commit_msg ?? r.ref),
    meta: "awaiting input",
  }
}

function ciRow(r: CIRunWithRepo): Row {
  const repo = slug(r.repo)
  return {
    key: `ci:${repo}:${r.number}`,
    href: `/${repo}/pipelines/${r.number}`,
    repo,
    num: r.number,
    label: r.commit_msg ?? r.ref,
    meta: `${r.ref} · ${r.status}`,
  }
}

function pullRow(p: PullRequestWithRepo): Row {
  const repo = slug(p.repo)
  return {
    key: `pull:${repo}:${p.number}`,
    href: `/${repo}/pulls/${p.number}`,
    repo,
    num: p.number,
    label: p.title,
    meta: `${p.head_ref} → ${p.base_ref}`,
  }
}

function issueRow(i: IssueWithRepo): Row {
  const repo = slug(i.repo)
  return {
    key: `issue:${repo}:${i.number}`,
    href: `/${repo}/issues/${i.number}`,
    repo,
    num: i.number,
    label: i.title,
    meta: "",
  }
}
