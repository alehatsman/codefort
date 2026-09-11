import { useDroppable } from "@dnd-kit/core"
import { useVirtualizer } from "@tanstack/react-virtual"
import clsx from "clsx"
import { useRef, useState } from "react"
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

// All columns collapse past this threshold behind a "Show N more" toggle.
const COL_VISIBLE = 30

/**
 * Drop target for one state. The header shows state name + count; the body
 * lists draggable cards with virtual scroll. Visual highlight when something
 * is hovering over it.
 *
 * All columns collapse items past {@link COL_VISIBLE} behind a "Show N more"
 * toggle. The `done` column additionally sorts by recency (updated_at desc)
 * so recently-closed work surfaces first. Within the visible window the body
 * is a fixed-height virtualised scroll area — only rendered cards near the
 * viewport are in the DOM, keeping large columns lightweight.
 */
export default function BoardColumn({ state, items, showRepo = false }: Props) {
  const { setNodeRef, isOver } = useDroppable({
    id: `column-${state}`,
    data: { state },
  })
  const [expanded, setExpanded] = useState(false)
  const bodyRef = useRef<HTMLDivElement>(null)

  const isDone = state === "done"
  const ordered = isDone
    ? [...items].sort((a, b) => +new Date(b.issue.updated_at) - +new Date(a.issue.updated_at))
    : items
  const collapsible = ordered.length > COL_VISIBLE
  const visible = collapsible && !expanded ? ordered.slice(0, COL_VISIBLE) : ordered
  const hiddenCount = ordered.length - visible.length

  // Estimate each card height: title (~40px) + meta row (~24px) + gap (8px) + border/padding (~16px).
  const CARD_EST = 88
  const rowVirtualizer = useVirtualizer({
    count: visible.length,
    getScrollElement: () => bodyRef.current,
    estimateSize: () => CARD_EST,
    overscan: 5,
  })

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
      <div ref={bodyRef} className="board-col__body">
        {items.length === 0 && <div className="board-col__empty muted">No issues</div>}
        {items.length > 0 && (
          <div className="board-col__virtual" style={{ height: rowVirtualizer.getTotalSize() }}>
            {rowVirtualizer.getVirtualItems().map((vItem) => {
              const item = visible[vItem.index]
              if (!item) return null
              const { issue, owner, repo } = item
              return (
                <div
                  key={vItem.key}
                  className="board-col__vrow"
                  style={{ transform: `translateY(${vItem.start}px)` }}
                  ref={rowVirtualizer.measureElement}
                  data-index={vItem.index}
                >
                  <BoardCard owner={owner} repo={repo} issue={issue} showRepo={showRepo} />
                </div>
              )
            })}
          </div>
        )}
        {collapsible && (
          <button type="button" className="board-col__more" onClick={() => setExpanded((v) => !v)}>
            {expanded ? "Show less" : `Show ${hiddenCount} more`}
          </button>
        )}
      </div>
    </div>
  )
}
