import { lazy, Suspense } from "react"
import { useDeleteComment } from "@/api/mutations"
import type { Comment } from "@/api/types"
import { Avatar, ErrorMessage } from "@/ui"

// The markdown renderer pulls in remark/rehype + the highlighter; load it lazily
// so the comment list doesn't drag it into the main bundle.
const Markdown = lazy(() => import("@/shell/Markdown"))

interface Props {
  owner: string
  repo: string
  issueNumber: number
  comment: Comment
  /** True when the current user authored this comment. */
  canDelete: boolean
}

/**
 * Single comment with a delete affordance shown only to the author.
 * Uses window.confirm for the destructive-action prompt — minimal
 * but clear; a custom modal would be more scope than this slice
 * needs.
 */
export default function CommentItem({ owner, repo, issueNumber, comment, canDelete }: Props) {
  const del = useDeleteComment(owner, repo, issueNumber)

  function onDelete() {
    if (!confirm("Delete this comment? This cannot be undone.")) return
    del.mutate(comment.id)
  }

  return (
    <li className="comment">
      <span className="comment__avatar">
        <Avatar name={comment.author} />
      </span>
      <div className="comment__card">
        <div className="comment__head">
          <strong>{comment.author}</strong>
          <span className="muted">
            commented on {new Date(comment.created_at).toLocaleString()}
          </span>
          {canDelete && (
            <button
              type="button"
              className="comment__delete"
              disabled={del.isPending}
              onClick={onDelete}
              title="Delete comment"
              aria-label="Delete comment"
            >
              {del.isPending ? "…" : "×"}
            </button>
          )}
        </div>
        <div className="comment__body">
          <Suspense fallback={<div className="markdown-body loading">Loading…</div>}>
            <Markdown content={comment.body} owner={owner} repo={repo} basePath="" />
          </Suspense>
        </div>
        {del.error && <ErrorMessage error={del.error} inline />}
      </div>
    </li>
  )
}
