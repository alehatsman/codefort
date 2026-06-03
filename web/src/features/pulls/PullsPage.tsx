import { useEffect, useState } from "react"
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom"
import "./pulls.css"
import { usePulls } from "@/api/queries"
import { PR_STATES, type PRState } from "@/api/types"
import OverviewCard from "@/shell/OverviewCard"
import PullsFilters from "@/features/pulls/PullsFilters"
import { Button, EmptyState, ErrorMessage, Spinner } from "@/ui"

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

  // Keyword search lives in the URL as ?q=, matched against title/body by the
  // server. Debounced into the param (like the issue list) so typing doesn't
  // refetch on every keystroke; the committed value drives the query.
  const committedQuery = params.get("q") ?? ""
  const [search, setSearch] = useState(committedQuery)
  useEffect(() => {
    const trimmed = search.trim()
    if (trimmed === committedQuery) return
    const t = setTimeout(() => {
      setParams(
        (prev) => {
          const p = new URLSearchParams(prev)
          if (trimmed) p.set("q", trimmed)
          else p.delete("q")
          return p
        },
        { replace: true }
      )
    }, 250)
    return () => clearTimeout(t)
  }, [search, committedQuery, setParams])

  // Empty selection => no state param => the server returns every state, the
  // same "no filter = all" behavior the issue list has.
  const { data, isLoading, error } = usePulls(owner, repo, activeStates.join(","), committedQuery)

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
      <OverviewCard owner={owner} repo={repo} path="" summaries={{}} />

      <div className="issues__header">
        <div className="issues__header-left">
          <h2>Pull requests</h2>
        </div>
        <Button variant="primary" onClick={() => navigate(`/${owner}/${repo}/compare`)}>
          + New pr
        </Button>
      </div>

      <PullsFilters
        search={search}
        onSearchChange={setSearch}
        activeStates={activeStates}
        onToggleState={toggleState}
      />

      {isLoading && <Spinner />}
      {error && <ErrorMessage error={error} />}

      {data && data.length === 0 && <EmptyState>No pull requests match this filter.</EmptyState>}

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
