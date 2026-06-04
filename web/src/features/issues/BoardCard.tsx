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
 * Pure visual card — no drag wiring. Used by DragOverlay so the floating
 * drag preview renders at portal level without registering a second draggable.
 */
export function BoardCardDisplay({ owner, repo, issue, showRepo = false }: Props) {
  return (
    <div className="board-card">
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
        {issue.labels.length > 0 && (
          <div className="issue-labels">
            {issue.labels.map((l) => (
              <span key={l} className="issue-label">
                {l}
              </span>
            ))}
          </div>
        )}
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

/**
 * One issue rendered as a draggable card. The whole card is the drag handle;
 * clicking it navigates to the issue. The synthetic click the browser fires
 * after a drop is swallowed by BoardPage's document-level capture listener,
 * which survives the card remounting into its new column.
 *
 * While dragging, the original card becomes a translucent ghost — the floating
 * preview is handled by DragOverlay in the parent, which renders at portal
 * level and is never clipped by the column's overflow-y:auto container.
 */
export default function BoardCard({ owner, repo, issue, showRepo = false }: Props) {
  const { attributes, listeners, setNodeRef, isDragging } = useDraggable({
    id: `issue-${issue.id}`,
    // owner/repo ride along so the cross-repo board can PATCH the right repo on
    // drop; the per-repo board ignores them and uses its own scope.
    data: { issueNumber: issue.number, currentState: issue.state, owner, repo },
  })

  return (
    <div
      ref={setNodeRef}
      className="board-card"
      style={{ opacity: isDragging ? 0.4 : 1 }}
      {...listeners}
      {...attributes}
    >
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
        {issue.labels.length > 0 && (
          <div className="issue-labels">
            {issue.labels.map((l) => (
              <span key={l} className="issue-label">
                {l}
              </span>
            ))}
          </div>
        )}
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
