import { useEffect, useMemo, useRef, useState } from "react"
import "./issues.css"
import {
  closestCenter,
  DndContext,
  type DragEndEvent,
  DragOverlay,
  type DragStartEvent,
  PointerSensor,
  useSensor,
  useSensors,
} from "@dnd-kit/core"
import { useQueryClient } from "@tanstack/react-query"
import { useParams } from "react-router-dom"
import { api } from "@/api/client"
import { keys, useIssues } from "@/api/queries"
import { ISSUE_STATES, type Issue, type IssueState } from "@/api/types"
import { BoardCardDisplay } from "@/features/issues/BoardCard"
import BoardColumn, { type BoardItem } from "@/features/issues/BoardColumn"
import IssuesViewSwitch from "@/features/issues/IssuesViewSwitch"
import NewIssueForm from "@/features/issues/NewIssueForm"
import OverviewCard from "@/shell/OverviewCard"
import { ErrorMessage, FilterBar, PageHeader, Spinner } from "@/ui"

/**
 * Trello-style board view. Columns are the four issue states; cards
 * are issues. Drag a card to a different column → PATCH state via
 * the existing endpoint. Click a card → navigate to the issue.
 *
 * Pulls all (non-paginated) issues — limit=1000 is the server's hard
 * cap. The board view is targeted at personal projects, not at repos
 * with thousands of issues.
 */
export default function BoardPage() {
  const { owner = "", repo = "" } = useParams()
  const qc = useQueryClient()

  // Fetch everything that hasn't been excluded by limit.
  const issuesQ = useIssues(owner, repo, "limit=1000")

  const [search, setSearch] = useState("")

  // Group issues by state once per data change. Each card carries this repo's
  // owner/repo so BoardColumn stays repo-agnostic (shared with the global board).
  const grouped = useMemo(() => {
    const map: Record<IssueState, BoardItem[]> = {
      todo: [],
      in_progress: [],
      done: [],
      closed: [],
    }
    const q = search.trim().toLowerCase()
    issuesQ.data
      ?.filter((iss) => !q || iss.title.toLowerCase().includes(q))
      .forEach((iss) => {
        map[iss.state].push({ issue: iss, owner, repo })
      })
    return map
  }, [issuesQ.data, owner, repo, search])

  const [activeItem, setActiveItem] = useState<BoardItem | null>(null)

  // 6px activation distance so quick clicks stay clicks. Trello convention.
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 6 } }))

  // After a drop the browser fires a synthetic click on the released card,
  // which would navigate via its <Link>. A per-card guard can't catch it (the
  // card remounts when it changes column, resetting any ref), and a React
  // capture handler is unreliable because the click's target node is mid-
  // remount. So swallow the next click with a native document-level capture
  // listener — it runs before navigation and before React routes the event.
  const justDraggedRef = useRef(false)
  useEffect(() => {
    function swallowPostDragClick(e: MouseEvent) {
      // biome-ignore lint/suspicious/noUnnecessaryConditions: false positive — the ref is mutated in onDragStart, a different closure the analyzer doesn't see
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
    const targetState = over.data.current?.["state"] as IssueState | undefined
    const currentState = active.data.current?.["currentState"] as IssueState | undefined
    const issueNumber = active.data.current?.["issueNumber"] as number | undefined
    if (!targetState || !issueNumber || !currentState || targetState === currentState) return

    // Fire the mutation directly so we can rollback the optimistic
    // update on error without coupling to the useSetIssueState hook
    // (its mutationFn closes over a static `n`).
    optimisticallyMoveAndPatch(owner, repo, issueNumber, currentState, targetState, qc)
  }

  if (issuesQ.isLoading) return <Spinner label="Loading board…" />
  if (issuesQ.error) return <ErrorMessage error={issuesQ.error} />

  return (
    <div>
      <OverviewCard owner={owner} repo={repo} path="" />

      <PageHeader title="Issues" actions={<NewIssueForm owner={owner} repo={repo} />}>
        <IssuesViewSwitch />
      </PageHeader>

      <FilterBar
        search={search}
        onSearch={setSearch}
        searchPlaceholder="Search issues…"
        searchAriaLabel="Search issues"
      />

      <DndContext
        sensors={sensors}
        collisionDetection={closestCenter}
        onDragStart={onDragStart}
        onDragEnd={onDragEnd}
      >
        <div className="board">
          {ISSUE_STATES.map((state) => (
            <BoardColumn key={state} state={state} items={grouped[state]} />
          ))}
        </div>
        <DragOverlay>
          {activeItem && (
            <BoardCardDisplay
              owner={activeItem.owner}
              repo={activeItem.repo}
              issue={activeItem.issue}
            />
          )}
        </DragOverlay>
      </DndContext>
    </div>
  )
}

/**
 * Optimistic move: edit the cached issues list immediately so the card
 * jumps columns without waiting on the network, then PATCH. Rollback
 * the cache on error.
 */
function optimisticallyMoveAndPatch(
  owner: string,
  repo: string,
  number: number,
  fromState: IssueState,
  toState: IssueState,
  qc: ReturnType<typeof useQueryClient>
) {
  // Snapshot all issue-list queries (varies by filter string).
  const cacheKey = keys.issues(owner, repo, "limit=1000")
  const prev = qc.getQueryData<Issue[]>(cacheKey)
  if (prev) {
    qc.setQueryData<Issue[]>(
      cacheKey,
      prev.map((iss) => (iss.number === number ? { ...iss, state: toState } : iss))
    )
  }

  api
    .updateIssue(owner, repo, number, { state: toState })
    .then(() => {
      // Server-confirmed. Invalidate to pick up updated_at + counts.
      qc.invalidateQueries({ queryKey: keys.issues(owner, repo) })
      qc.invalidateQueries({ queryKey: keys.repos() })
      qc.invalidateQueries({ queryKey: keys.repo(owner, repo) })
      qc.invalidateQueries({ queryKey: keys.issue(owner, repo, number) })
    })
    .catch((err) => {
      console.error("state PATCH failed, rolling back:", err)
      if (prev) qc.setQueryData(cacheKey, prev)
      // Show the error via invalidation — the next fetch surfaces server truth.
      qc.invalidateQueries({ queryKey: keys.issues(owner, repo) })
    })

  // Suppress unused-from warning — kept for symmetry / future telemetry.
  void fromState
}
