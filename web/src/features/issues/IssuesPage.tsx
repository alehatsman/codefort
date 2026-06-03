import clsx from "clsx"
import { useEffect, useMemo, useState } from "react"
import "./issues.css"
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom"
import { useIssues } from "@/api/queries"
import { ISSUE_STATES, type IssueState } from "@/api/types"
import NewIssueForm from "@/features/issues/NewIssueForm"
import OverviewCard from "@/shell/OverviewCard"
import StateIcon from "@/features/issues/StateIcon"
import IssuesViewSwitch from "@/features/issues/IssuesViewSwitch"
import { useListNav } from "@/shell/keyboardNav"
import { EmptyState, ErrorMessage, FilterChip, Spinner } from "@/ui"

const IssuesPage = () => {
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
  const sort = searchParams.get("sort") ?? "newest"
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

  const query = new URLSearchParams()
  if (activeStates.length > 0) query.set("state", activeStates.join(","))
  if (assignee) query.set("assignee", assignee)
  if (author) query.set("author", author)
  if (committedQuery) query.set("q", committedQuery)
  if (sort !== "newest") query.set("sort", sort)

  const { data, isLoading, error } = useIssues(owner, repo, query.toString())

  // Option lists are derived from the issues currently returned (no separate
  // endpoint — v1). The active selection is always included so it stays
  // visible even when the current filter excludes every row carrying it.
  const authorOptions = useMemo(() => {
    const set = new Set<string>()
    for (const iss of data ?? []) set.add(iss.author)
    if (author) set.add(author)
    return [...set].sort()
  }, [data, author])

  const assigneeOptions = useMemo(() => {
    const set = new Set<string>()
    for (const iss of data ?? []) if (iss.assignee) set.add(iss.assignee)
    if (assignee && assignee !== "null") set.add(assignee)
    return [...set].sort()
  }, [data, assignee])

  // j/k select an issue row and Enter opens it. (h/l tab nav lives in RepoTabs.)
  const { index } = useListNav({
    count: data?.length ?? 0,
    onActivate: (i) => {
      const iss = data?.[i]
      if (iss) navigate(`/${owner}/${repo}/issues/${iss.number}`)
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
      navigate(`/${owner}/${repo}/issues/${m[1]}`)
    }
  }

  return (
    <div className="issues">
      <OverviewCard owner={owner} repo={repo} path="" summaries={{}} />

      <div className="issues__header">
        <div className="issues__header-left">
          <h2>Issues</h2>
          <IssuesViewSwitch />
        </div>
        <NewIssueForm
          owner={owner}
          repo={repo}
          onCreated={(n) => navigate(`/${owner}/${repo}/issues/${n}`)}
        />
      </div>

      <div className="filters">
        <input
          type="search"
          className="list-search"
          placeholder="Search title or body, or #number…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          onKeyDown={onSearchKeyDown}
          aria-label="Search issues"
        />
        <div className="filter-row">
          <span className="filter-label">state:</span>
          {ISSUE_STATES.map((s) => (
            <FilterChip key={s} checked={activeStates.includes(s)} onChange={() => toggleState(s)}>
              <StateIcon state={s} size={12} />
              {s}
            </FilterChip>
          ))}
        </div>
        <div className="filter-row">
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
        </div>
      </div>

      {isLoading && <Spinner />}
      {error && <ErrorMessage error={error} />}

      {data && data.length === 0 && <EmptyState>No issues match these filters.</EmptyState>}

      {data && data.length > 0 && (
        <ul className="issue-list">
          {data.map((iss, i) => (
            <li
              key={iss.id}
              className={clsx("issue-row", { "is-vim-selected": i === index })}
              data-vim-selected={i === index ? "true" : undefined}
            >
              <Link to={`/${owner}/${repo}/issues/${iss.number}`} className="issue-row__link">
                <span className="issue-row__icon">
                  <StateIcon state={iss.state} />
                </span>
                <span className="issue-row__main">
                  <span className="issue-row__title">{iss.title}</span>
                  <span className="issue-row__meta">
                    #{iss.number} opened {new Date(iss.created_at).toLocaleDateString()} by{" "}
                    {iss.author}
                  </span>
                </span>
                <span className="issue-row__side">{iss.assignee ? `@${iss.assignee}` : ""}</span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

export default IssuesPage
