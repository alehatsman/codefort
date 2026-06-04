import { useEffect, useMemo, useState } from "react"
import { useSearchParams } from "react-router-dom"
import "./pulls.css"
import { useAllPulls } from "@/api/queries"
import { PR_STATES, type PRState } from "@/api/types"
import NewGlobalPullForm from "@/features/pulls/NewGlobalPullForm"
import PullsFilters from "@/features/pulls/PullsFilters"
import { repoHue } from "@/shell/repoColor"
import { useRepoFilter } from "@/shell/useRepoFilter"
import { EmptyState, ErrorMessage, ListRow, PageHeader, Spinner } from "@/ui"

const STATE_LABEL: Record<PRState, string> = {
  open: "open",
  merged: "merged",
  closed: "closed",
}

// No state param => the default view (open only), matching PullsPage.
const DEFAULT_STATES: readonly PRState[] = ["open"]

// Fleet-wide Pull requests view: every repo's PRs in one list, newest-updated
// first, each row tagged with and linking into its owning repo. Filterable by
// state, keyword, and repo.
export default function GlobalPullsPage() {
  const [params, setParams] = useSearchParams()
  const { activeRepos, toggleRepo } = useRepoFilter()

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

  const availableRepos = useMemo(
    () => [...new Set((data ?? []).map((p) => `${p.repo.owner}/${p.repo.name}`))].sort(),
    [data]
  )

  const pulls = useMemo(() => {
    if (!data) return []
    if (activeRepos.length === 0) return data
    return data.filter((p) => activeRepos.includes(`${p.repo.owner}/${p.repo.name}`))
  }, [data, activeRepos])

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
      <PageHeader title="Pull requests" actions={<NewGlobalPullForm />} />

      <PullsFilters
        search={search}
        onSearchChange={setSearch}
        searchPlaceholder="Search title or body across all repos…"
        activeStates={activeStates}
        onToggleState={toggleState}
        availableRepos={availableRepos}
        activeRepos={activeRepos}
        onToggleRepo={toggleRepo}
      />

      {isLoading && <Spinner />}
      <ErrorMessage error={error} />

      {data && pulls.length === 0 && <EmptyState>No pull requests match this filter.</EmptyState>}

      {data && pulls.length > 0 && (
        <ul className="issue-list">
          {pulls.map((pr) => (
            <ListRow
              key={`${pr.repo.owner}/${pr.repo.name}#${pr.number}`}
              to={`/${pr.repo.owner}/${pr.repo.name}/pulls/${pr.number}`}
              leading={
                <span className={`pr-state pr-state--${pr.state}`}>{STATE_LABEL[pr.state]}</span>
              }
              title={pr.title}
              meta={
                <>
                  <span
                    className="repo-tag"
                    style={
                      {
                        "--repo-hue": repoHue(`${pr.repo.owner}/${pr.repo.name}`),
                      } as React.CSSProperties
                    }
                  >
                    {pr.repo.owner}/{pr.repo.name}
                  </span>{" "}
                  #{pr.number} {pr.head_ref} → {pr.base_ref} · opened{" "}
                  {new Date(pr.created_at).toLocaleDateString()} by {pr.author}
                </>
              }
            />
          ))}
        </ul>
      )}
    </div>
  )
}
