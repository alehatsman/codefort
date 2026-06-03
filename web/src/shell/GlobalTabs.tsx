import clsx from "clsx"
import { Link, useLocation } from "react-router-dom"

/**
 * Top-level (non-repo) navigation, rendered inline in the global top bar by
 * Layout whenever the URL has no repo context. It mirrors RepoTabs but the
 * tabs are fleet-wide aggregate views: Repos (the index), and the cross-repo
 * Issues / Pull requests / Pipelines / Agents feeds. The active tab is derived
 * from the current URL.
 */
export default function GlobalTabs() {
  const { pathname } = useLocation()
  const isRepos = pathname === "/"
  const isIssues = pathname.startsWith("/issues")
  const isPulls = pathname.startsWith("/pulls")
  const isPipelines = pathname.startsWith("/pipelines")
  const isAgents = pathname.startsWith("/agents")

  return (
    <nav className="tabs" aria-label="Global navigation">
      <Link to="/" className={clsx("tab", { "is-active": isRepos })}>
        Repos
      </Link>
      <Link to="/issues" className={clsx("tab", { "is-active": isIssues })}>
        Issues
      </Link>
      <Link to="/pulls" className={clsx("tab", { "is-active": isPulls })}>
        Pull requests
      </Link>
      <Link to="/pipelines" className={clsx("tab", { "is-active": isPipelines })}>
        Pipelines
      </Link>
      <Link to="/agents" className={clsx("tab", { "is-active": isAgents })}>
        Agents
      </Link>
    </nav>
  )
}
