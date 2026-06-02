import { Link, useSearchParams } from "react-router-dom"
import { useAllPulls } from "../api/queries"
import { PR_STATES, type PRState } from "../api/types"

const STATE_LABEL: Record<PRState, string> = {
  open: "open",
  merged: "merged",
  closed: "closed",
}

// No state param => the default view (open only), matching PullsPage.
const DEFAULT_STATES: readonly PRState[] = ["open"]

// Fleet-wide Pull requests view: every repo's PRs in one list, newest-updated
// first, each row tagged with and linking into its owning repo. Mirrors the
// per-repo PullsPage state chips minus the repo-scoped chrome.
export default function GlobalPullsPage() {
  const [params, setParams] = useSearchParams()

  const raw = params.get("state")
  const activeStates: PRState[] =
    raw === null
      ? [...DEFAULT_STATES]
      : raw
          .split(",")
          .map((s) => s.trim())
          .filter((s): s is PRState => PR_STATES.includes(s as PRState))

  const { data, isLoading, error } = useAllPulls(activeStates.join(","))

  function toggleState(s: PRState) {
    const next = activeStates.includes(s)
      ? activeStates.filter((x) => x !== s)
      : [...activeStates, s]
    setParams(
      (prev) => {
        const p = new URLSearchParams(prev)
        p.set("state", next.join(","))
        return p
      },
      { replace: true }
    )
  }

  return (
    <div className="pulls">
      <div className="issues__header">
        <div className="issues__header-left">
          <h2>Pull requests</h2>
        </div>
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
            <li key={`${pr.repo.owner}/${pr.repo.name}#${pr.number}`} className="issue-row">
              <Link
                to={`/${pr.repo.owner}/${pr.repo.name}/pulls/${pr.number}`}
                className="issue-row__link"
              >
                <span className={`pr-state pr-state--${pr.state}`}>{STATE_LABEL[pr.state]}</span>
                <span className="issue-row__main">
                  <span className="issue-row__title">{pr.title}</span>
                  <span className="issue-row__meta">
                    <span className="issue-row__repo">
                      {pr.repo.owner}/{pr.repo.name}
                    </span>{" "}
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
