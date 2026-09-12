import { useEffect, useMemo, useState } from "react"
import "./issues.css"
import { useNavigate, useParams, useSearchParams } from "react-router-dom"
import { useIssuesPage } from "@/api/queries"
import { ISSUE_STATES, type IssueState } from "@/api/types"
import IssuesViewSwitch from "@/features/issues/IssuesViewSwitch"
import NewIssueForm from "@/features/issues/NewIssueForm"
import StateIcon from "@/features/issues/StateIcon"
import { useListNav } from "@/shell/keyboardNav"
import OverviewCard from "@/shell/OverviewCard"
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

// buildIssuesFilterQuery assembles the filter signature (everything except
// pagination). Pulled out to module scope so the run of ifs doesn't stack
// cognitive complexity on top of the component.
function buildIssuesFilterQuery(f: {
  activeStates: IssueState[]
  assignee: string
  author: string
  label: string
  committedQuery: string
  sort: string
  ready: boolean
  blocked: boolean
  epics: boolean
}): URLSearchParams {
  const q = new URLSearchParams()
  if (f.activeStates.length > 0) q.set("state", f.activeStates.join(","))
  if (f.assignee) q.set("assignee", f.assignee)
  if (f.author) q.set("author", f.author)
  if (f.label) q.set("label", f.label)
  if (f.committedQuery) q.set("q", f.committedQuery)
  if (f.sort !== "newest") q.set("sort", f.sort)
  if (f.ready) q.set("ready", "1")
  if (f.blocked) q.set("blocked", "1")
  if (f.epics) q.set("epics", "1")
  return q
}

export default function IssuesPage() {
  const { owner = "", repo = "" } = useParams()
  const navigate = useNavigate()
  const [activeStates, setActiveStates] = useState<IssueState[]>(["todo", "in_progress"])

  // Assignee / author / sort live in the URL so a filtered view is bookmarkable
  // (matching ?q=…). assignee: "" = any, "null" = unassigned, else an identity.
  // author: "" = any, else an identity. sort: "" defaults to newest.
  const [searchParams, setSearchParams] = useSearchParams()
  const committedQuery = searchParams.get("q") ?? ""
  const assignee = searchParams.get("assignee") ?? ""
  const author = searchParams.get("author") ?? ""
  const label = searchParams.get("label") ?? ""
  const sort = searchParams.get("sort") ?? "newest"
  const ready = searchParams.get("ready") === "1"
  const blocked = searchParams.get("blocked") === "1"
  const epics = searchParams.get("epics") === "1"
  const [search, setSearch] = useState(committedQuery)

  // Set or clear a single URL param without disturbing the others. An empty
  // value drops the param so the default view stays at a bare URL.
  function setParam(key: string, value: string) {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        if (value) next.set(key, value)
        else next.delete(key)
        return next
      },
      { replace: true }
    )
  }

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

  // The filter signature (everything except pagination). Page resets to 1
  // whenever it changes so a narrowed filter never strands you on a now-empty
  // page.
  const filterKey = buildIssuesFilterQuery({
    activeStates,
    assignee,
    author,
    label,
    committedQuery,
    sort,
    ready,
    blocked,
    epics,
  }).toString()

  // Reset to page 1 when the filter changes (React's adjust-state-during-render
  // pattern — no effect needed for derived resets).
  const [page, setPage] = useState(1)
  const [prevFilterKey, setPrevFilterKey] = useState(filterKey)
  if (filterKey !== prevFilterKey) {
    setPrevFilterKey(filterKey)
    setPage(1)
  }

  const query = new URLSearchParams(filterKey)
  query.set("limit", String(PAGE_SIZE))
  if (page > 1) query.set("offset", String((page - 1) * PAGE_SIZE))

  const { data, isLoading, error } = useIssuesPage(owner, repo, query.toString())
  const issues = data?.items ?? []
  const total = data?.total ?? 0

  // Option lists are derived from the issues currently returned (no separate
  // endpoint — v1). The active selection is always included so it stays
  // visible even when the current filter excludes every row carrying it.
  const authorOptions = useMemo(() => {
    const set = new Set<string>()
    for (const iss of issues) set.add(iss.author)
    if (author) set.add(author)
    return [...set].sort()
  }, [issues, author])

  const assigneeOptions = useMemo(() => {
    const set = new Set<string>()
    for (const iss of issues) if (iss.assignee) set.add(iss.assignee)
    if (assignee && assignee !== "null") set.add(assignee)
    return [...set].sort()
  }, [issues, assignee])

  const labelOptions = useMemo(() => {
    const set = new Set<string>()
    for (const iss of issues) for (const l of iss.labels) set.add(l)
    if (label) set.add(label)
    return [...set].sort()
  }, [issues, label])

  // j/k select an issue row and Enter opens it. (h/l tab nav lives in RepoTabs.)
  const { index } = useListNav({
    count: issues.length,
    onActivate: (i) => {
      const iss = issues[i]
      if (iss) void navigate(`/${owner}/${repo}/issues/${iss.number}`)
    },
  })

  function toggleState(s: IssueState) {
    setActiveStates((prev) => (prev.includes(s) ? prev.filter((x) => x !== s) : [...prev, s]))
  }

  // A bare "#42" (or "42") is a jump, not a search: Enter goes straight there.
  function onSearchKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key !== "Enter") return
    const m = search.trim().match(/^#?(\d+)$/)
    if (m) {
      e.preventDefault()
      void navigate(`/${owner}/${repo}/issues/${m[1]}`)
    }
  }

  return (
    <div className="issues">
      <OverviewCard owner={owner} repo={repo} path="" />

      <PageHeader
        title="Issues"
        actions={
          <NewIssueForm
            owner={owner}
            repo={repo}
            onCreated={(n) => navigate(`/${owner}/${repo}/issues/${n}`)}
          />
        }
      >
        <IssuesViewSwitch />
      </PageHeader>

      <FilterBar
        search={search}
        onSearch={setSearch}
        searchPlaceholder="Search title or body, or #number…"
        searchAriaLabel="Search issues"
        onSearchKeyDown={onSearchKeyDown}
      >
        <FilterRow label="state:">
          {ISSUE_STATES.map((s) => (
            <FilterChip key={s} checked={activeStates.includes(s)} onChange={() => toggleState(s)}>
              <StateIcon state={s} size={12} />
              {s}
            </FilterChip>
          ))}
        </FilterRow>
        <FilterRow>
          <label className="filter-select">
            <span className="filter-label">assignee:</span>
            <select
              value={assignee}
              onChange={(e) => setParam("assignee", e.target.value)}
              aria-label="Filter by assignee"
            >
              <option value="">any</option>
              <option value="null">unassigned</option>
              {assigneeOptions.map((a) => (
                <option key={a} value={a}>
                  @{a}
                </option>
              ))}
            </select>
          </label>
          <label className="filter-select">
            <span className="filter-label">author:</span>
            <select
              value={author}
              onChange={(e) => setParam("author", e.target.value)}
              aria-label="Filter by author"
            >
              <option value="">any</option>
              {authorOptions.map((a) => (
                <option key={a} value={a}>
                  {a}
                </option>
              ))}
            </select>
          </label>
          <label className="filter-select">
            <span className="filter-label">sort:</span>
            <select
              value={sort}
              onChange={(e) => setParam("sort", e.target.value === "newest" ? "" : e.target.value)}
              aria-label="Sort issues"
            >
              <option value="newest">newest</option>
              <option value="oldest">oldest</option>
              <option value="recently-updated">recently updated</option>
            </select>
          </label>
          {labelOptions.length > 0 && (
            <label className="filter-select">
              <span className="filter-label">label:</span>
              <select
                value={label}
                onChange={(e) => setParam("label", e.target.value)}
                aria-label="Filter by label"
              >
                <option value="">any</option>
                {labelOptions.map((l) => (
                  <option key={l} value={l}>
                    {l}
                  </option>
                ))}
              </select>
            </label>
          )}
        </FilterRow>
        <FilterRow label="view:">
          <FilterChip
            checked={epics}
            onChange={() => {
              setParam("epics", epics ? "" : "1")
              if (!epics) {
                setParam("ready", "")
                setParam("blocked", "")
              }
            }}
          >
            epics
          </FilterChip>
          <FilterChip
            checked={ready}
            onChange={() => {
              setParam("ready", ready ? "" : "1")
              if (!ready) setParam("epics", "")
            }}
          >
            ready
          </FilterChip>
          <FilterChip
            checked={blocked}
            onChange={() => {
              setParam("blocked", blocked ? "" : "1")
              if (!blocked) setParam("epics", "")
            }}
          >
            blocked
          </FilterChip>
        </FilterRow>
      </FilterBar>

      {isLoading && <SkeletonList />}
      {error && <ErrorMessage error={error} />}

      {data && issues.length === 0 && <EmptyState>No issues match these filters.</EmptyState>}

      {data && issues.length > 0 && (
        <>
          <ul className="issue-list">
            {issues.map((iss, i) => (
              <ListRow
                key={iss.id}
                to={`/${owner}/${repo}/issues/${iss.number}`}
                selected={i === index}
                leading={
                  <span className="issue-row__icon">
                    <StateIcon state={iss.state} />
                  </span>
                }
                title={iss.title}
                meta={
                  <>
                    #{iss.number} opened {new Date(iss.created_at).toLocaleDateString()} by{" "}
                    {iss.author}
                    {iss.labels.length > 0 && (
                      <span className="issue-labels">
                        {iss.labels.map((l) => (
                          <span key={l} className="issue-label">
                            {l}
                          </span>
                        ))}
                      </span>
                    )}
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
