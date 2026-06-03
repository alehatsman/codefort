import { useDraggable } from "@dnd-kit/core"
import { Link } from "react-router-dom"
import type { Issue } from "@/api/types"
import { Avatar } from "@/ui"

interface Props {
  owner: string
  repo: string
  issue: Issue
  // Show the owning repo on the card — used by the cross-repo (global) board.
  showRepo?: boolean
}

// Deterministic hue (0–359) from the repo key, so each project keeps a stable
// tag color across renders and reloads — same repo → same color. Plain string
// hash (djb2-ish), spread over the wheel.
function repoHue(key: string): number {
  let h = 0
  for (let i = 0; i < key.length; i++) h = (h * 31 + key.charCodeAt(i)) >>> 0
  return h % 360
}

/**
 * One issue rendered as a draggable card. The whole card is the drag handle;
 * clicking it navigates to the issue. The synthetic click the browser fires
 * after a drop is swallowed by BoardPage's document-level capture listener,
 * which survives the card remounting into its new column.
 */
export default function BoardCard({ owner, repo, issue, showRepo = false }: Props) {
  const { attributes, listeners, setNodeRef, transform, isDragging } = useDraggable({
    id: `issue-${issue.id}`,
    // owner/repo ride along so the cross-repo board can PATCH the right repo on
    // drop; the per-repo board ignores them and uses its own scope.
    data: { issueNumber: issue.number, currentState: issue.state, owner, repo },
  })

  const style: React.CSSProperties = {
    transform: transform ? `translate3d(${transform.x}px, ${transform.y}px, 0)` : undefined,
    opacity: isDragging ? 0.4 : 1,
  }

  return (
    <div ref={setNodeRef} className="board-card" style={style} {...listeners} {...attributes}>
      <Link to={`/${owner}/${repo}/issues/${issue.number}`} className="board-card__link">
        {showRepo && (
          <span
            className="repo-tag"
            style={{ "--repo-hue": repoHue(`${owner}/${repo}`) } as React.CSSProperties}
          >
            {owner}/{repo}
          </span>
        )}
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
