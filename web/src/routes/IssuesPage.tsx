import { useEffect, useState } from "react"
import { Link, useNavigate, useParams, useSearchParams } from "react-router-dom"
import { useIssues, useRepo } from "../api/queries"
import { ISSUE_STATES, type IssueState } from "../api/types"
import NewIssueForm from "../components/NewIssueForm"
import RepoHeader from "../components/RepoHeader"
import OverviewCard from "../components/OverviewCard"
import StateIcon from "../components/StateIcon"
import IssuesViewSwitch from "../components/IssuesViewSwitch"
import { useListNav } from "../lib/keyboardNav"

export default function IssuesPage() {
  const { owner = "", repo = "" } = useParams()
  const navigate = useNavigate()
  const [activeStates, setActiveStates] = useState<IssueState[]>(["todo", "in_progress"])
  const [unassignedOnly, setUnassignedOnly] = useState(false)

  // The committed search term lives in the URL (?q=…) so a filtered view is
  // bookmarkable, matching the list/board convention. `search` is the live
  // input value; it's debounced into the URL so we don't refetch per keystroke.
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

  const repoQ = useRepo(owner, repo)

  const query = new URLSearchParams()
  if (activeStates.length > 0) query.set("state", activeStates.join(","))
  if (unassignedOnly) query.set("assignee", "null")
  if (committedQuery) query.set("q", committedQuery)

  const { data, isLoading, error } = useIssues(owner, repo, query.toString())

  // j/k select an issue row and Enter opens it. (h/l tab nav lives in RepoHeader.)
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
      <RepoHeader owner={owner} repo={repo} openIssues={repoQ.data?.open_issues} />
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
          className="issues__search"
          placeholder="Search title or body, or #number…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          onKeyDown={onSearchKeyDown}
          aria-label="Search issues"
        />
        <div className="filter-row">
          <span className="filter-label">state:</span>
          {ISSUE_STATES.map((s) => (
            <label key={s} className="chip">
              <input
                type="checkbox"
                checked={activeStates.includes(s)}
                onChange={() => toggleState(s)}
              />
              <StateIcon state={s} size={12} />
              {s}
            </label>
          ))}
        </div>
        <label className="chip">
          <input
            type="checkbox"
            checked={unassignedOnly}
            onChange={(e) => setUnassignedOnly(e.target.checked)}
          />
          unassigned only
        </label>
      </div>

      {isLoading && <div className="loading">Loading…</div>}
      {error && <div className="error">{(error as Error).message}</div>}

      {data && data.length === 0 && <div className="empty">No issues match these filters.</div>}

      {data && data.length > 0 && (
        <ul className="issue-list">
          {data.map((iss, i) => (
            <li
              key={iss.id}
              className={`issue-row ${i === index ? "is-vim-selected" : ""}`}
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
