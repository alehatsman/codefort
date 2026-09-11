import { useEffect, useMemo, useState } from "react"
import { useNavigate, useSearchParams } from "react-router-dom"
import "./issues.css"
import { useAllIssuesPage } from "@/api/queries"
import { ISSUE_STATES, type IssueState } from "@/api/types"
import IssuesViewSwitch from "@/features/issues/IssuesViewSwitch"
import NewIssueForm from "@/features/issues/NewIssueForm"
import StateIcon from "@/features/issues/StateIcon"
import { useListNav } from "@/shell/keyboardNav"
import { repoHue } from "@/shell/repoColor"
import { useRepoFilter } from "@/shell/useRepoFilter"
import {
  EmptyState,
  ErrorMessage,
  FilterBar,
  FilterChip,
  FilterRow,
  ListRow,
  PageHeader,
  Pagination,
  SkeletonList,
} from "@/ui"

const PAGE_SIZE = 25

// Fleet-wide Issues view: every repo's issues in one list, newest-updated
// first, each row tagged with and linking into its owning repo. Filterable by
// state, keyword, and repo (repo filter is client-side on the loaded page).
export default function GlobalIssuesPage() {
  const navigate = useNavigate()
  const [activeStates, setActiveStates] = useState<IssueState[]>(["todo", "in_progress"])
  const { activeRepos, toggleRepo } = useRepoFilter()

  const [searchParams, setSearchParams] = useSearchParams()
  const committedQuery = searchParams.get("q") ?? ""
  const [search, setSearch] = useState(committedQuery)

  useEffect(() => {
    const trimmed = search.trim()
    if (trimmed === committedQuery) return
    const t = setTimeout(() => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          if (trimmed) next.set("q", trimmed)
          else next.delete("q")
          return next
        },
        { replace: true }
      )
    }, 250)
    return () => clearTimeout(t)
  }, [search, committedQuery, setSearchParams])

  // Filter signature (no pagination); page resets to 1 when it changes.
  // Include repo filter in filterKey so page resets when repos are toggled.
  const filterQuery = new URLSearchParams()
  if (activeStates.length > 0) filterQuery.set("state", activeStates.join(","))
  if (committedQuery) filterQuery.set("q", committedQuery)
  if (activeRepos.length > 0) filterQuery.set("_repo", activeRepos.join(","))
  const filterKey = filterQuery.toString()

  // Reset to page 1 when the filter changes (adjust-state-during-render).
  const [page, setPage] = useState(1)
  const [prevFilterKey, setPrevFilterKey] = useState(filterKey)
  if (filterKey !== prevFilterKey) {
    setPrevFilterKey(filterKey)
    setPage(1)
  }

  const query = new URLSearchParams()
  if (activeStates.length > 0) query.set("state", activeStates.join(","))
  if (committedQuery) query.set("q", committedQuery)
  query.set("limit", String(PAGE_SIZE))
  if (page > 1) query.set("offset", String((page - 1) * PAGE_SIZE))

  const { data, isLoading, error } = useAllIssuesPage(query.toString())
  const allIssues = data?.items ?? []
  const total = data?.total ?? 0

  const availableRepos = useMemo(
    () => [...new Set(allIssues.map((i) => `${i.repo.owner}/${i.repo.name}`))].sort(),
    [allIssues]
  )

  const issues = useMemo(() => {
    if (activeRepos.length === 0) return allIssues
    return allIssues.filter((i) => activeRepos.includes(`${i.repo.owner}/${i.repo.name}`))
  }, [allIssues, activeRepos])

  const { index } = useListNav({
    count: issues.length,
    onActivate: (i) => {
      const iss = issues[i]
      if (iss) void navigate(`/${iss.repo.owner}/${iss.repo.name}/issues/${iss.number}`)
    },
  })

  function toggleState(s: IssueState) {
    setActiveStates((prev) => (prev.includes(s) ? prev.filter((x) => x !== s) : [...prev, s]))
  }

  return (
    <div className="issues">
      <PageHeader
        title="Issues"
        actions={<NewIssueForm onCreated={(n, o, r) => navigate(`/${o}/${r}/issues/${n}`)} />}
      >
        <IssuesViewSwitch />
      </PageHeader>

      <FilterBar
        search={search}
        onSearch={setSearch}
        searchPlaceholder="Search title or body across all repos…"
        searchAriaLabel="Search issues"
      >
        <FilterRow label="state:">
          {ISSUE_STATES.map((s) => (
            <FilterChip key={s} checked={activeStates.includes(s)} onChange={() => toggleState(s)}>
              <StateIcon state={s} size={12} />
              {s}
            </FilterChip>
          ))}
        </FilterRow>
        {availableRepos.length > 0 && (
          <FilterRow label="repo:">
            {availableRepos.map((r) => (
              <FilterChip key={r} checked={activeRepos.includes(r)} onChange={() => toggleRepo(r)}>
                <span
                  className="repo-dot"
                  style={{ "--repo-hue": repoHue(r) } as React.CSSProperties}
                />
                {r}
              </FilterChip>
            ))}
          </FilterRow>
        )}
      </FilterBar>

      {isLoading && <SkeletonList />}
      {error && <ErrorMessage error={error} />}

      {data && issues.length === 0 && <EmptyState>No issues match these filters.</EmptyState>}

      {data && issues.length > 0 && (
        <>
          <ul className="issue-list">
            {issues.map((iss, i) => (
              <ListRow
                key={`${iss.repo.owner}/${iss.repo.name}#${iss.number}`}
                to={`/${iss.repo.owner}/${iss.repo.name}/issues/${iss.number}`}
                selected={i === index}
                leading={
                  <span className="issue-row__icon">
                    <StateIcon state={iss.state} />
                  </span>
                }
                title={iss.title}
                meta={
                  <>
                    <span
                      className="repo-tag"
                      style={
                        {
                          "--repo-hue": repoHue(`${iss.repo.owner}/${iss.repo.name}`),
                        } as React.CSSProperties
                      }
                    >
                      {iss.repo.owner}/{iss.repo.name}
                    </span>{" "}
                    #{iss.number} opened {new Date(iss.created_at).toLocaleDateString()} by{" "}
                    {iss.author}
                  </>
                }
                side={iss.assignee ? `@${iss.assignee}` : ""}
              />
            ))}
          </ul>
          <Pagination page={page} pageSize={PAGE_SIZE} total={total} onPageChange={setPage} />
        </>
      )}
    </div>
  )
}
