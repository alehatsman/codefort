import clsx from "clsx"
import "./pulls.css"
import { lazy, Suspense, useMemo } from "react"
import { Link, useParams, useSearchParams } from "react-router-dom"
import { useBlob, useCodeComments, useWhoami } from "@/api/queries"
import { useDeleteCodeComment, useSetCodeCommentResolved } from "@/api/mutations"
import BranchSelector from "@/features/repo/BranchSelector"
import DraftReviewButton from "@/features/pulls/DraftReviewButton"
import {
  Avatar,
  Badge,
  Button,
  Checkbox,
  CodeSnippet,
  EmptyState,
  ErrorMessage,
  Spinner,
} from "@/ui"
import type { CodeComment, CodeCommentState } from "@/api/types"

// Lines of context shown above and below each comment's annotated range, so a
// snippet reads like a slice cut from the blob viewer rather than the bare
// commented lines.
const CONTEXT_LINES = 3

// The markdown renderer pulls in remark/rehype + the highlighter; load it lazily
// so the review list doesn't drag it into the main bundle.
const Markdown = lazy(() => import("@/shell/Markdown"))

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
            <Checkbox
              key={s}
              className="chip"
              label={s}
              checked={checked[s]}
              onChange={() => toggleState(s)}
            />
          ))}
        </div>
        <DraftReviewButton owner={owner} repo={repo} defaultRef={gitRef} />
      </div>

      {commentsQ.isLoading && <Spinner />}
      <ErrorMessage error={commentsQ.error} />
      {commentsQ.data && comments.length === 0 && (
        <EmptyState>No {state === "all" ? "" : `${state} `}comments on this branch.</EmptyState>
      )}

      {byPath.map(([path, list]) => (
        <ReviewFile
          key={path}
          owner={owner}
          repo={repo}
          gitRef={gitRef}
          path={path}
          comments={list}
          currentUser={whoamiQ.data?.name}
        />
      ))}
    </div>
  )
}

// One file's comments, each shown as a snippet cut from the file with the
// comment beneath it. The blob is fetched once per file and sliced per comment.
function ReviewFile({
  owner,
  repo,
  gitRef,
  path,
  comments,
  currentUser,
}: {
  owner: string
  repo: string
  gitRef: string
  path: string
  comments: CodeComment[]
  currentUser?: string
}) {
  const blobQ = useBlob(owner, repo, path, gitRef)

  // The file's source lines on this ref, computed once for slicing each
  // comment's context. Null while loading or when the blob can't render as text
  // (binary / too large) — rows then fall back to the server-provided snippet.
  const fileLines = useMemo(() => {
    const b = blobQ.data
    if (!b || b.binary || b.too_large) return null
    const body = b.content.endsWith("\n") ? b.content.slice(0, -1) : b.content
    return body.split("\n")
  }, [blobQ.data])

  return (
    <div className="review-file">
      <div className="review-file__head">
        <Link to={blobHref(owner, repo, path, gitRef)} className="review-file__path">
          {path}
        </Link>
        <span className="muted small">{comments.length}</span>
      </div>
      <ul className="review-list">
        {comments.map((c) => (
          <ReviewRow
            key={c.id}
            owner={owner}
            repo={repo}
            gitRef={gitRef}
            comment={c}
            canManage={!!currentUser && currentUser === c.author}
            fileLines={fileLines}
          />
        ))}
      </ul>
    </div>
  )
}

function ReviewRow({
  owner,
  repo,
  gitRef,
  comment,
  canManage,
  fileLines,
}: {
  owner: string
  repo: string
  gitRef: string
  comment: CodeComment
  canManage: boolean
  fileLines: string[] | null
}) {
  const resolve = useSetCodeCommentResolved(owner, repo)
  const del = useDeleteCodeComment(owner, repo)
  const lines =
    comment.end_line > comment.start_line
      ? `L${comment.start_line}-L${comment.end_line}`
      : `L${comment.start_line}`

  // A slice cut from the file around the comment's range (±CONTEXT_LINES),
  // clamped to the file bounds. Null when the blob isn't usable or the anchor
  // points past the current file (a stale ref) — the row then falls back to the
  // server snippet.
  const context = useMemo(() => {
    if (!fileLines || comment.start_line > fileLines.length) return null
    const from = Math.max(1, comment.start_line - CONTEXT_LINES)
    const to = Math.min(fileLines.length, comment.end_line + CONTEXT_LINES)
    return { code: fileLines.slice(from - 1, to).join("\n"), from }
  }, [fileLines, comment.start_line, comment.end_line])

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
        {comment.resolved && <Badge state="done">resolved</Badge>}
        {canManage && (
          <span className="review-row__actions">
            <Button
              variant="ghost"
              size="small"
              disabled={resolve.isPending}
              onClick={() => resolve.mutate({ id: comment.id, resolved: !comment.resolved })}
            >
              {comment.resolved ? "Reopen" : "Resolve"}
            </Button>
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
      {context ? (
        <CodeSnippet
          className="review-row__code"
          code={context.code}
          path={comment.path}
          startLine={context.from}
          focus={[comment.start_line, comment.end_line]}
        />
      ) : (
        comment.snippet && <pre className="review-row__snippet">{comment.snippet}</pre>
      )}
      <div className="review-row__body">
        <Suspense fallback={<div className="markdown-body loading">Loading…</div>}>
          <Markdown content={comment.body} owner={owner} repo={repo} basePath="" />
        </Suspense>
      </div>
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
