import { lazy, Suspense } from "react"
import { useDeleteComment } from "@/api/mutations"
import type { Comment as CommentData } from "@/api/types"
import { Comment, ErrorMessage, Tooltip } from "@/ui"

// The markdown renderer pulls in remark/rehype + the highlighter; load it lazily
// so the comment list doesn't drag it into the main bundle.
const Markdown = lazy(() => import("@/shell/Markdown"))

interface Props {
  owner: string
  repo: string
  issueNumber: number
  comment: CommentData
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
    <Comment
      author={comment.author}
      meta={`commented on ${new Date(comment.created_at).toLocaleString()}`}
      actions={
        canDelete && (
          <Tooltip label="Delete comment">
            <button
              type="button"
              className="comment__delete"
              disabled={del.isPending}
              onClick={onDelete}
              aria-label="Delete comment"
            >
              {del.isPending ? "…" : "×"}
            </button>
          </Tooltip>
        )
      }
      footer={del.error && <ErrorMessage error={del.error} inline />}
    >
      <Suspense fallback={<div className="markdown-body loading">Loading…</div>}>
        <Markdown content={comment.body} owner={owner} repo={repo} basePath="" />
      </Suspense>
    </Comment>
  )
}
