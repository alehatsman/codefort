import { useEffect, useMemo, useRef, useState } from "react"
import "./issues.css"
import { useNavigate } from "react-router-dom"
import {
  DndContext,
  DragOverlay,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragStartEvent,
} from "@dnd-kit/core"
import { useQueryClient } from "@tanstack/react-query"
import { api } from "@/api/client"
import { keys, useAllIssues } from "@/api/queries"
import { ISSUE_STATES, type IssueState, type IssueWithRepo } from "@/api/types"
import BoardColumn, { type BoardItem } from "@/features/issues/BoardColumn"
import { BoardCardDisplay } from "@/features/issues/BoardCard"
import StateIcon from "@/features/issues/StateIcon"
import IssuesViewSwitch from "@/features/issues/IssuesViewSwitch"
import NewIssueForm from "@/features/issues/NewIssueForm"
import { useRepoFilter } from "@/shell/useRepoFilter"
import { ErrorMessage, FilterBar, FilterChip, FilterRow, PageHeader, Spinner } from "@/ui"

// Same hard cap as the per-repo board — the board is the visualization, not a
// paginated list; fleet scale here is hundreds, not thousands.
const ALL_ISSUES_QUERY = "limit=1000"

/**
 * Fleet-wide Trello board: every repo's issues in one set of state columns.
 * Cards carry their owning repo, so dragging a card across columns PATCHes the
 * correct repo's issue. Mirrors BoardPage but cross-repo (no OverviewCard, and
 * cards show owner/repo). Filterable by keyword, repo, and visible columns.
 */
export default function GlobalBoardPage() {
  const navigate = useNavigate()
  const qc = useQueryClient()

  const issuesQ = useAllIssues(ALL_ISSUES_QUERY)

  const [search, setSearch] = useState("")
  const { activeRepos, toggleRepo } = useRepoFilter()
  // hiddenStates: columns toggled off. Default empty = all columns visible.
  const [hiddenStates, setHiddenStates] = useState<Set<IssueState>>(new Set())

  const availableRepos = useMemo(
    () => [...new Set((issuesQ.data ?? []).map((i) => `${i.repo.owner}/${i.repo.name}`))].sort(),
    [issuesQ.data]
  )

  const filtered = useMemo(() => {
    let items = issuesQ.data ?? []
    const q = search.trim().toLowerCase()
    if (q) items = items.filter((i) => i.title.toLowerCase().includes(q))
    if (activeRepos.length > 0)
      items = items.filter((i) => activeRepos.includes(`${i.repo.owner}/${i.repo.name}`))
    return items
  }, [issuesQ.data, search, activeRepos])

  const grouped = useMemo(() => {
    const map: Record<IssueState, BoardItem[]> = {
      todo: [],
      in_progress: [],
      done: [],
      closed: [],
    }
    filtered.forEach((iss) => {
      map[iss.state].push({ issue: iss, owner: iss.repo.owner, repo: iss.repo.name })
    })
    return map
  }, [filtered])

  const visibleStates = ISSUE_STATES.filter((s) => !hiddenStates.has(s))

  function toggleColumn(state: IssueState) {
    setHiddenStates((prev) => {
      const next = new Set(prev)
      if (next.has(state)) next.delete(state)
      else next.add(state)
      return next
    })
  }

  const [activeItem, setActiveItem] = useState<BoardItem | null>(null)

  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 6 } }))

  // Swallow the synthetic post-drop click so a drag doesn't navigate. See
  // BoardPage for the full rationale (card remounts across columns).
  const justDraggedRef = useRef(false)
  useEffect(() => {
    function swallowPostDragClick(e: MouseEvent) {
      if (justDraggedRef.current) {
        justDraggedRef.current = false
        e.preventDefault()
        e.stopPropagation()
      }
    }
    document.addEventListener("click", swallowPostDragClick, true)
    return () => document.removeEventListener("click", swallowPostDragClick, true)
  }, [])

  function onDragStart(event: DragStartEvent) {
    justDraggedRef.current = true
    const { issueNumber, owner: o, repo: r } = event.active.data.current ?? {}
    if (!issueNumber || !o || !r) return
    for (const items of Object.values(grouped)) {
      const found = items.find(
        (item) => item.issue.number === issueNumber && item.owner === o && item.repo === r
      )
      if (found) {
        setActiveItem(found)
        return
      }
    }
  }

  function onDragEnd(event: DragEndEvent) {
    setActiveItem(null)
    const { active, over } = event
    if (!over) return
    const targetState = over.data.current?.state as IssueState | undefined
    const currentState = active.data.current?.currentState as IssueState | undefined
    const issueNumber = active.data.current?.issueNumber as number | undefined
    const owner = active.data.current?.owner as string | undefined
    const repo = active.data.current?.repo as string | undefined
    if (!targetState || !issueNumber || !currentState || !owner || !repo) return
    if (targetState === currentState) return

    optimisticallyMoveAndPatch(owner, repo, issueNumber, targetState, qc)
  }

  if (issuesQ.isLoading) return <Spinner label="Loading board…" />
  if (issuesQ.error) return <ErrorMessage error={issuesQ.error} />

  return (
    <div>
      <PageHeader
        title="Issues"
        actions={<NewIssueForm onCreated={(n, o, r) => navigate(`/${o}/${r}/issues/${n}`)} />}
      >
        <IssuesViewSwitch />
      </PageHeader>

      <FilterBar
        search={search}
        onSearch={setSearch}
        searchPlaceholder="Search issues across all repos…"
        searchAriaLabel="Search issues"
      >
        <FilterRow label="columns:">
          {ISSUE_STATES.map((s) => (
            <FilterChip key={s} checked={!hiddenStates.has(s)} onChange={() => toggleColumn(s)}>
              <StateIcon state={s} size={12} />
              {s}
            </FilterChip>
          ))}
        </FilterRow>
        {availableRepos.length > 0 && (
          <FilterRow label="repo:">
            {availableRepos.map((r) => (
              <FilterChip key={r} checked={activeRepos.includes(r)} onChange={() => toggleRepo(r)}>
                {r}
              </FilterChip>
            ))}
          </FilterRow>
        )}
      </FilterBar>

      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        onDragStart={onDragStart}
        onDragEnd={onDragEnd}
      >
        <div className="board">
          {visibleStates.map((state) => (
            <BoardColumn key={state} state={state} items={grouped[state]} showRepo />
          ))}
        </div>
        <DragOverlay>
          {activeItem && (
            <BoardCardDisplay
              owner={activeItem.owner}
              repo={activeItem.repo}
              issue={activeItem.issue}
              showRepo
            />
          )}
        </DragOverlay>
      </DndContext>
    </div>
  )
}

/**
 * Optimistic cross-repo move: patch the cached aggregate list immediately, then
 * PATCH the owning repo's issue. Rollback on error. Mirrors BoardPage's helper
 * but keyed on the allIssues cache and matching rows by repo + number (issue
 * numbers are per-repo, so number alone isn't unique across repos).
 */
function optimisticallyMoveAndPatch(
  owner: string,
  repo: string,
  number: number,
  toState: IssueState,
  qc: ReturnType<typeof useQueryClient>
) {
  const cacheKey = keys.allIssues(ALL_ISSUES_QUERY)
  const prev = qc.getQueryData<IssueWithRepo[]>(cacheKey)
  if (prev) {
    qc.setQueryData<IssueWithRepo[]>(
      cacheKey,
      prev.map((iss) =>
        iss.repo.owner === owner && iss.repo.name === repo && iss.number === number
          ? { ...iss, state: toState }
          : iss
      )
    )
  }

  api
    .updateIssue(owner, repo, number, { state: toState })
    .then(() => {
      qc.invalidateQueries({ queryKey: keys.allIssues() })
      qc.invalidateQueries({ queryKey: keys.issues(owner, repo) })
      qc.invalidateQueries({ queryKey: keys.repos() })
      qc.invalidateQueries({ queryKey: keys.issue(owner, repo, number) })
    })
    .catch((err) => {
      console.error("state PATCH failed, rolling back:", err)
      if (prev) qc.setQueryData(cacheKey, prev)
      qc.invalidateQueries({ queryKey: keys.allIssues() })
    })
}
