import clsx from "clsx"
import { Link, useLocation } from "react-router-dom"
import { useTabNav } from "../lib/keyboardNav"

interface Props {
  owner: string
  repo: string
  openIssues?: number
}

/**
 * Tab bar shown across all repo-scoped routes. The active tab is derived from
 * the current URL. "Code" covers the repo root plus the /tree/ and /blob/
 * browser routes. List/Board views both live under Issues — the switch between
 * them is rendered inside the Issues pages.
 *
 * Repo identity (owner/repo) is not shown here: it leads the unified
 * breadcrumb in the OverviewCard each page renders just below the tabs.
 *
 * Living on every repo route, this is also where h/l tab navigation is
 * wired (`useTabNav`), so the keys work consistently across all tabs.
 */
export default function RepoHeader({ owner, repo, openIssues }: Props) {
  const location = useLocation()
  useTabNav(owner, repo)
  const base = `/${owner}/${repo}`
  const isCode =
    location.pathname === base ||
    location.pathname.startsWith(`${base}/tree/`) ||
    location.pathname.startsWith(`${base}/blob/`) ||
    location.pathname.startsWith(`${base}/commits`)
  const isIssues = location.pathname.startsWith(`${base}/issues`)
  const isPulls =
    location.pathname.startsWith(`${base}/pulls`) || location.pathname.startsWith(`${base}/compare`)
  const isReview = location.pathname.startsWith(`${base}/review`)
  // Explore absorbed the former Research + Summaries tabs (and their URLs).
  const isExplore =
    location.pathname.startsWith(`${base}/explore`) ||
    location.pathname.startsWith(`${base}/research`) ||
    location.pathname.startsWith(`${base}/summaries`)
  const isPipelines = location.pathname.startsWith(`${base}/pipelines`)

  return (
    <div className="repo-header">
      <nav className="tabs" aria-label="Repository navigation">
        <Link to={base} className={clsx("tab", { "is-active": isCode })}>
          Code
        </Link>
        <Link to={`${base}/issues`} className={clsx("tab", { "is-active": isIssues })}>
          Issues
          {openIssues !== undefined && <span className="tab__count">{openIssues}</span>}
        </Link>
        <Link to={`${base}/pulls`} className={clsx("tab", { "is-active": isPulls })}>
          Pull requests
        </Link>
        <Link to={`${base}/review`} className={clsx("tab", { "is-active": isReview })}>
          Review
        </Link>
        <Link to={`${base}/explore`} className={clsx("tab", { "is-active": isExplore })}>
          Explore
        </Link>
        <Link to={`${base}/pipelines`} className={clsx("tab", { "is-active": isPipelines })}>
          Pipelines
        </Link>
      </nav>
    </div>
  )
}
