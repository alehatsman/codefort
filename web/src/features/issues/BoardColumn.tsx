import { useState } from "react"
import { useDroppable } from "@dnd-kit/core"
import clsx from "clsx"
import type { Issue, IssueState } from "@/api/types"
import BoardCard from "@/features/issues/BoardCard"
import StateIcon from "@/features/issues/StateIcon"

// One card's worth of data. Each item carries its own owner/repo so the same
// column serves both the per-repo board (all items share a repo) and the
// cross-repo global board (items span repos).
export interface BoardItem {
  issue: Issue
  owner: string
  repo: string
}

interface Props {
  state: IssueState
  items: BoardItem[]
  // Render the owning repo on each card (cross-repo board).
  showRepo?: boolean
}

// The done column can pile up; keep it scannable by showing only the most
// recent N and collapsing the rest behind a toggle.
const DONE_VISIBLE = 15

/**
 * Drop target for one state. The header shows state name + count; the body
 * lists draggable cards. Visual highlight when something is hovering over it.
 *
 * The `done` column sorts by recency (updated_at desc — the move-to-done time)
 * and collapses everything past the first {@link DONE_VISIBLE} behind a
 * "Show N more" toggle, so a long backlog of finished work stays tidy.
 */
export default function BoardColumn({ state, items, showRepo = false }: Props) {
  const { setNodeRef, isOver } = useDroppable({
    id: `column-${state}`,
    data: { state },
  })
  const [expanded, setExpanded] = useState(false)

  const isDone = state === "done"
  const ordered = isDone
    ? [...items].sort((a, b) => +new Date(b.issue.updated_at) - +new Date(a.issue.updated_at))
    : items
  const collapsible = isDone && ordered.length > DONE_VISIBLE
  const visible = collapsible && !expanded ? ordered.slice(0, DONE_VISIBLE) : ordered
  const hiddenCount = ordered.length - visible.length

  return (
    <div
      ref={setNodeRef}
      className={clsx("board-col", { "is-over": isOver })}
      data-testid={`board-column-${state}`}
    >
      <div className={`board-col__head board-col__head--${state}`}>
        <StateIcon state={state} size={14} />
        <span className="board-col__name">{state.replace("_", " ")}</span>
        <span className="board-col__count">{items.length}</span>
      </div>
      <div className="board-col__body">
        {items.length === 0 && <div className="board-col__empty muted">No issues</div>}
        {visible.map(({ issue, owner, repo }) => (
          <BoardCard key={issue.id} owner={owner} repo={repo} issue={issue} showRepo={showRepo} />
        ))}
        {collapsible && (
          <button type="button" className="board-col__more" onClick={() => setExpanded((v) => !v)}>
            {expanded ? "Show less" : `Show ${hiddenCount} more`}
          </button>
        )}
      </div>
    </div>
  )
}
