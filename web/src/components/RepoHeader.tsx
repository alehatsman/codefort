import { Link, useLocation } from "react-router-dom"
import { useTabNav } from "../lib/keyboardNav"

interface Props {
  owner: string
  repo: string
  openIssues?: number
}

/**
 * Title + tab bar shown across all repo-scoped routes. The active tab
 * is derived from the current URL. "Code" covers the repo root plus the
 * /tree/ and /blob/ browser routes. List/Board views both live under
 * Issues — the switch between them is rendered inside the Issues pages.
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
  const isIntel = location.pathname.startsWith(`${base}/intel`)

  return (
    <div className="repo-header">
      <h1 className="page-title">
        <Link to="/" className="page-title__owner">
          {owner}
        </Link>
        <span className="page-title__sep">/</span>
        <Link to={base} className="page-title__name">
          {repo}
        </Link>
      </h1>

      <nav className="tabs" aria-label="Repository navigation">
        <Link to={base} className={`tab ${isCode ? "is-active" : ""}`}>
          Code
        </Link>
        <Link to={`${base}/issues`} className={`tab ${isIssues ? "is-active" : ""}`}>
          Issues
          {openIssues !== undefined && <span className="tab__count">{openIssues}</span>}
        </Link>
        <Link to={`${base}/intel`} className={`tab ${isIntel ? "is-active" : ""}`}>
          Intel
        </Link>
      </nav>
    </div>
  )
}
