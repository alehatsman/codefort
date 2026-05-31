import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom"
import { usePulls, useRepo } from "../api/queries"
import { PR_STATES, type PRState } from "../api/types"
import OverviewCard from "../components/OverviewCard"
import PRStateIcon from "../components/PRStateIcon"
import RepoHeader from "../components/RepoHeader"

const STATE_LABEL: Record<PRState, string> = {
  open: "open",
  merged: "merged",
  closed: "closed",
}

// No state param => the default view (open only), matching the issue list's
// "active states" default rather than showing everything.
const DEFAULT_STATES: readonly PRState[] = ["open"]

export default function PullsPage() {
  const { owner = "", repo = "" } = useParams()
  const navigate = useNavigate()
  const [params, setParams] = useSearchParams()

  // State filter lives in the URL as a comma-joined ?state= (the server's
  // parsePRStates splits it), mirroring the issue list's checkbox chips. An
  // absent param is the default; an explicit (even empty) param is honored so
  // an all-unchecked selection round-trips instead of snapping back to default.
  const raw = params.get("state")
  const activeStates: PRState[] =
    raw === null
      ? [...DEFAULT_STATES]
      : raw
        .split(",")
        .map((s) => s.trim())
        .filter((s): s is PRState => PR_STATES.includes(s as PRState))

  const repoQ = useRepo(owner, repo)
  // Empty selection => no state param => the server returns every state, the
  // same "no filter = all" behavior the issue list has.
  const { data, isLoading, error } = usePulls(owner, repo, activeStates.join(","))

  function toggleState(s: PRState) {
    const next = activeStates.includes(s)
      ? activeStates.filter((x) => x !== s)
      : [...activeStates, s]
    setParams(
      (prev) => {
        const p = new URLSearchParams(prev)
        // Persist the selection verbatim — an empty value (state=) is distinct
        // from an absent param (the default), so unchecking all sticks.
        p.set("state", next.join(","))
        return p
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
          + New pull request
        </button>
      </div>

      <div className="filters">
        <div className="filter-row">
          <span className="filter-label">state:</span>
          {PR_STATES.map((s) => (
            <label key={s} className="chip">
              <input
                type="checkbox"
                checked={activeStates.includes(s)}
                onChange={() => toggleState(s)}
              />
              <PRStateIcon state={s} size={12} />
              {s}
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
