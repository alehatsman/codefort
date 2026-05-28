import { useEffect, useRef } from "react"
import { useDraggable } from "@dnd-kit/core"
import { Link } from "react-router-dom"
import type { Issue } from "../api/types"
import Avatar from "./Avatar"

interface Props {
  owner: string
  repo: string
  issue: Issue
}

/**
 * One issue rendered as a draggable card. The whole card is the drag
 * handle. A subtle issue: after a drag-and-drop, the browser still
 * fires a synthetic `click` on the released element, and by that time
 * dnd-kit's `isDragging` has already flipped back to false. So we
 * track "we just dragged" in a ref that survives the drop→click hop
 * and swallow that one click. Subsequent clicks navigate normally,
 * preserving Link semantics (right-click, ctrl/cmd-click, etc.).
 */
export default function BoardCard({ owner, repo, issue }: Props) {
  const { attributes, listeners, setNodeRef, transform, isDragging } = useDraggable({
    id: `issue-${issue.id}`,
    data: { issueNumber: issue.number, currentState: issue.state },
  })

  const justDraggedRef = useRef(false)
  useEffect(() => {
    if (isDragging) justDraggedRef.current = true
  }, [isDragging])

  function onLinkClick(e: React.MouseEvent) {
    if (justDraggedRef.current) {
      justDraggedRef.current = false
      e.preventDefault()
    }
  }

  const style: React.CSSProperties = {
    transform: transform ? `translate3d(${transform.x}px, ${transform.y}px, 0)` : undefined,
    opacity: isDragging ? 0.4 : 1,
  }

  return (
    <div ref={setNodeRef} className="board-card" style={style} {...listeners} {...attributes}>
      <Link
        to={`/${owner}/${repo}/issues/${issue.number}`}
        className="board-card__link"
        onClick={onLinkClick}
      >
        <div className="board-card__title">{issue.title}</div>
        <div className="board-card__meta">
          <span className="muted">#{issue.number}</span>
          {issue.assignee && (
            <span className="board-card__assignee">
              <Avatar name={issue.assignee} />
            </span>
          )}
        </div>
      </Link>
    </div>
  )
}
