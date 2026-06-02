import clsx from "clsx"
import { Link, useParams, useSearchParams } from "react-router-dom"
import { useCodeComments, useWhoami } from "../api/queries"
import { useDeleteCodeComment, useSetCodeCommentResolved } from "../api/mutations"
import BranchSelector from "../components/BranchSelector"
import DraftReviewButton from "../components/DraftReviewButton"
import Avatar from "../components/Avatar"
import type { CodeComment, CodeCommentState } from "../api/types"

// The two real comment states. Both checked (or neither) => "all"; the
// checkbox set maps onto the server's single ?state= (open|resolved|all),
// keeping this filter visually identical to the issue list's chips.
const COMMENT_STATES = ["open", "resolved"] as const

// Derive the server state from which chips are checked. Neither checked falls
// back to "all" (no filter = everything), mirroring the issue list.
function deriveState(open: boolean, resolved: boolean): CodeCommentState {
  if (open === resolved) return "all"
  return open ? "open" : "resolved"
}

/**
 * Review tab: every code comment on the selected branch, grouped by file, each
 * deep-linking into the blob viewer at its line range. The branch comes from
 * the shared `?ref=` param (same selector as the code browser); `?state=`
 * filters open / resolved / all. This is the surface a reviewer — or Claude via
 * `mgit review list` — works through.
 */
export default function ReviewPage() {
  const { owner = "", repo = "" } = useParams()
  const [params, setParams] = useSearchParams()
  const gitRef = params.get("ref") ?? ""
  const state = (params.get("state") as CodeCommentState) || "open"

  const commentsQ = useCodeComments(owner, repo, { ref: gitRef, state })
  const whoamiQ = useWhoami()

  const checkedOpen = state === "open" || state === "all"
  const checkedResolved = state === "resolved" || state === "all"

  // Toggle one chip, recompute the server state, and write it to ?state=
  // ("open" is the default, so it drops the param to keep the URL bare).
  function toggleState(which: (typeof COMMENT_STATES)[number]) {
    const open = which === "open" ? !checkedOpen : checkedOpen
    const resolved = which === "resolved" ? !checkedResolved : checkedResolved
    const s = deriveState(open, resolved)
    const next = new URLSearchParams(params)
    if (s === "open") next.delete("state")
    else next.set("state", s)
    setParams(next, { replace: true })
  }

  const checked: Record<(typeof COMMENT_STATES)[number], boolean> = {
    open: checkedOpen,
    resolved: checkedResolved,
  }

  const comments = commentsQ.data ?? []
  const byPath = groupByPath(comments)

  return (
    <div className="repo">
      <h2 className="issue-title">Review comments</h2>

      <div className="repo-toolbar">
        <BranchSelector owner={owner} repo={repo} />
        <div className="filter-row">
          <span className="filter-label">state:</span>
          {COMMENT_STATES.map((s) => (
            <label key={s} className="chip">
              <input type="checkbox" checked={checked[s]} onChange={() => toggleState(s)} />
              {s}
            </label>
          ))}
        </div>
        <DraftReviewButton owner={owner} repo={repo} defaultRef={gitRef} />
      </div>

      {commentsQ.isLoading && <div className="loading">Loading…</div>}
      {commentsQ.error && <div className="error">{(commentsQ.error as Error).message}</div>}
      {commentsQ.data && comments.length === 0 && (
        <div className="empty">No {state === "all" ? "" : `${state} `}comments on this branch.</div>
      )}

      {byPath.map(([path, list]) => (
        <div key={path} className="review-file">
          <div className="review-file__head">
            <Link to={blobHref(owner, repo, path, gitRef)} className="review-file__path">
              {path}
            </Link>
            <span className="muted small">{list.length}</span>
          </div>
          <ul className="review-list">
            {list.map((c) => (
              <ReviewRow
                key={c.id}
                owner={owner}
                repo={repo}
                gitRef={gitRef}
                comment={c}
                canManage={!!whoamiQ.data && whoamiQ.data.name === c.author}
              />
            ))}
          </ul>
        </div>
      ))}
    </div>
  )
}

function ReviewRow({
  owner,
  repo,
  gitRef,
  comment,
  canManage,
}: {
  owner: string
  repo: string
  gitRef: string
  comment: CodeComment
  canManage: boolean
}) {
  const resolve = useSetCodeCommentResolved(owner, repo)
  const del = useDeleteCodeComment(owner, repo)
  const lines =
    comment.end_line > comment.start_line
      ? `L${comment.start_line}-L${comment.end_line}`
      : `L${comment.start_line}`

  return (
    <li className={clsx("review-row", { "is-resolved": comment.resolved })}>
      <div className="review-row__head">
        <Avatar name={comment.author} />
        <strong>{comment.author}</strong>
        <Link
          to={`${blobHref(owner, repo, comment.path, gitRef)}#${lines}`}
          className="review-row__lines"
        >
          {comment.path}:{lines}
        </Link>
        {comment.resolved && <span className="badge badge--done">resolved</span>}
        {canManage && (
          <span className="review-row__actions">
            <button
              type="button"
              className="btn btn--ghost btn--sm"
              disabled={resolve.isPending}
              onClick={() => resolve.mutate({ id: comment.id, resolved: !comment.resolved })}
            >
              {comment.resolved ? "Reopen" : "Resolve"}
            </button>
            <button
              type="button"
              className="comment__delete"
              disabled={del.isPending}
              onClick={() => {
                if (confirm("Delete this comment? This cannot be undone.")) del.mutate(comment.id)
              }}
              aria-label="Delete comment"
            >
              {del.isPending ? "…" : "×"}
            </button>
          </span>
        )}
      </div>
      <div className="review-row__body">{comment.body}</div>
      {comment.snippet && <pre className="review-row__snippet">{comment.snippet}</pre>}
    </li>
  )
}

// Group comments by file path, preserving the server's path-then-line order.
function groupByPath(comments: CodeComment[]): [string, CodeComment[]][] {
  const map = new Map<string, CodeComment[]>()
  for (const c of comments) {
    const arr = map.get(c.path) ?? []
    arr.push(c)
    map.set(c.path, arr)
  }
  return [...map.entries()]
}

function blobHref(owner: string, repo: string, path: string, gitRef: string): string {
  const base = `/${owner}/${repo}/blob/${path}`
  return gitRef ? `${base}?ref=${encodeURIComponent(gitRef)}` : base
}
