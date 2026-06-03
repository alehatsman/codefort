import { useEffect, useState } from "react"
import { Link, useSearchParams } from "react-router-dom"
import "./pulls.css"
import { useAllPulls } from "@/api/queries"
import { PR_STATES, type PRState } from "@/api/types"
import NewGlobalPullForm from "@/features/pulls/NewGlobalPullForm"
import PullsFilters from "@/features/pulls/PullsFilters"
import { EmptyState, ErrorMessage, Spinner } from "@/ui"

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

  // Keyword search lives in the URL as ?q=, debounced into the param so typing
  // doesn't refetch on every keystroke; mirrors GlobalIssuesPage.
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

  const { data, isLoading, error } = useAllPulls(activeStates.join(","), committedQuery)

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
        <NewGlobalPullForm />
      </div>

      <PullsFilters
        search={search}
        onSearchChange={setSearch}
        searchPlaceholder="Search title or body across all repos…"
        activeStates={activeStates}
        onToggleState={toggleState}
      />

      {isLoading && <Spinner />}
      <ErrorMessage error={error} />

      {data && data.length === 0 && <EmptyState>No pull requests match this filter.</EmptyState>}

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
