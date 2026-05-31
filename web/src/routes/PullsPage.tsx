import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom"
import { usePulls, useRepo } from "../api/queries"
import type { PRState } from "../api/types"
import OverviewCard from "../components/OverviewCard"
import RepoHeader from "../components/RepoHeader"

// PR list states the filter offers, plus "all" (no state param). Mirrors the
// issue list's chip filter, scaled down to the PR lifecycle.
const FILTERS: readonly { value: string; label: string }[] = [
  { value: "open", label: "open" },
  { value: "merged", label: "merged" },
  { value: "closed", label: "closed" },
  { value: "all", label: "all" },
]

const STATE_LABEL: Record<PRState, string> = {
  open: "open",
  merged: "merged",
  closed: "closed",
}

export default function PullsPage() {
  const { owner = "", repo = "" } = useParams()
  const navigate = useNavigate()
  const [params, setParams] = useSearchParams()
  const filter = params.get("state") ?? "open"

  const repoQ = useRepo(owner, repo)
  // "all" omits the state param entirely; any other value is sent through.
  const { data, isLoading, error } = usePulls(owner, repo, filter === "all" ? "" : filter)

  function setFilter(value: string) {
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        if (value === "open") next.delete("state")
        else next.set("state", value)
        return next
      },
      { replace: true }
    )
  }

  return (
    <div className="pulls">
      <RepoHeader owner={owner} repo={repo} openIssues={repoQ.data?.open_issues} />
      <OverviewCard owner={owner} repo={repo} path="" summaries={{}} />

      <div className="issues__header">
        <div className="issues__header-left">
          <h2>Pull requests</h2>
        </div>
        <button
          type="button"
          className="btn btn--primary"
          onClick={() => navigate(`/${owner}/${repo}/compare`)}
        >
          New pull request
        </button>
      </div>

      <div className="filters">
        <div className="filter-row">
          <span className="filter-label">state:</span>
          {FILTERS.map((f) => (
            <label key={f.value} className="chip">
              <input
                type="radio"
                name="pr-state"
                checked={filter === f.value}
                onChange={() => setFilter(f.value)}
              />
              {f.label}
            </label>
          ))}
        </div>
      </div>

      {isLoading && <div className="loading">Loading…</div>}
      {error && <div className="error">{(error as Error).message}</div>}

      {data && data.length === 0 && (
        <div className="empty">No pull requests match this filter.</div>
      )}

      {data && data.length > 0 && (
        <ul className="issue-list">
          {data.map((pr) => (
            <li key={pr.id} className="issue-row">
              <Link to={`/${owner}/${repo}/pulls/${pr.number}`} className="issue-row__link">
                <span className={`pr-state pr-state--${pr.state}`}>{STATE_LABEL[pr.state]}</span>
                <span className="issue-row__main">
                  <span className="issue-row__title">{pr.title}</span>
                  <span className="issue-row__meta">
                    #{pr.number} {pr.head_ref} → {pr.base_ref} · opened{" "}
                    {new Date(pr.created_at).toLocaleDateString()} by {pr.author}
                  </span>
                </span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
