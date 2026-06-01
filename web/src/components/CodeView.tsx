import clsx from "clsx"
import { lazy, Suspense, useEffect, useMemo, useRef, useState } from "react"
import type { ReactNode } from "react"
import { useLocation } from "react-router-dom"
import { highlight, langFromPath, splitLines } from "../lib/highlight"
import {
  useCreateCodeComment,
  useDeleteCodeComment,
  useSetCodeCommentResolved,
} from "../api/mutations"
import type { CodeComment } from "../api/types"
import Avatar from "./Avatar"

// The markdown renderer pulls in remark/rehype; load it lazily.
const Markdown = lazy(() => import("./Markdown"))

interface Props {
  content: string
  /** Repo-relative path of the file; picks a highlight grammar and anchors
   *  comments. */
  path?: string
  /** Commenting turns on only when owner+repo+path are all present. */
  owner?: string
  repo?: string
  /** Branch the comments are bound to ("" => the repo default). */
  codeRef?: string
  /** Existing comments for this file on this ref. */
  comments?: CodeComment[]
  /** Authenticated user's name, for author-only resolve/delete affordances. */
  currentUser?: string
}

/**
 * Line-numbered source viewer in GitHub's blob style. The whole file is
 * tokenised once (so multi-line tokens such as block comments stay correct),
 * then the token tree is sliced into per-line fragments. Token colours come
 * from the Primer palette in styles.css.
 *
 * On top of the viewer it carries review comments anchored to a line range:
 * press a line number and drag down the gutter to select a range (or click one
 * line, shift-click another), and an inline form opens beneath the selection on
 * release. Existing comments
 * render inline under the line they end on, each with author-only Resolve and
 * Delete. A `#L<n>`/`#L<start>-L<end>` URL hash still deep-links + highlights.
 */
export default function CodeView({
  content,
  path,
  owner,
  repo,
  codeRef = "",
  comments = [],
  currentUser,
}: Props) {
  const lines = useMemo(() => {
    const body = content.endsWith("\n") ? content.slice(0, -1) : content
    return splitLines(highlight(body, path ? langFromPath(path) : undefined))
  }, [content, path])

  const { hash } = useLocation()
  const [from, to] = parseLineRange(hash)
  const tableRef = useRef<HTMLTableElement>(null)

  const commentsEnabled = !!owner && !!repo && !!path

  // Active selection (anchor + head, both 1-based). The compose form opens
  // beneath the selection's last line once the drag is released.
  const [sel, setSel] = useState<{ anchor: number; head: number } | null>(null)
  const [dragging, setDragging] = useState(false)
  const selStart = sel ? Math.min(sel.anchor, sel.head) : 0
  const selEnd = sel ? Math.max(sel.anchor, sel.head) : 0

  // `lines` is a re-scroll trigger, not read here: once async content renders
  // the rows, we re-run so #L<from> exists to scroll into view.
  // biome-ignore lint/correctness/useExhaustiveDependencies: lines drives the rows this effect queries
  useEffect(() => {
    if (from == null) return
    tableRef.current?.querySelector<HTMLElement>(`#L${from}`)?.scrollIntoView({ block: "center" })
  }, [from, lines])

  // End the drag wherever the button is released — including off the gutter —
  // so a release outside a line number still finalizes the range.
  useEffect(() => {
    if (!dragging) return
    const stop = () => setDragging(false)
    window.addEventListener("mouseup", stop)
    return () => window.removeEventListener("mouseup", stop)
  }, [dragging])

  // Existing comments grouped by the line they end on, so each renders right
  // under the block it annotates.
  const byEndLine = useMemo(() => {
    const m = new Map<number, CodeComment[]>()
    for (const c of comments) {
      const arr = m.get(c.end_line) ?? []
      arr.push(c)
      m.set(c.end_line, arr)
    }
    return m
  }, [comments])

  // Every line covered by a saved comment, so the range a thread annotates stays
  // tinted after the transient drag selection clears (else, once the comment
  // posts, nothing shows which lines it belongs to).
  const commentedLines = useMemo(() => {
    const s = new Set<number>()
    for (const c of comments) {
      for (let n = c.start_line; n <= c.end_line; n++) s.add(n)
    }
    return s
  }, [comments])

  // Drag-to-select on the gutter (GitHub style): press a line number to anchor,
  // drag over others to extend live, release to finalize. Shift-press extends
  // an existing selection without a drag. preventDefault keeps the drag from
  // starting a native text selection.
  function onNumMouseDown(e: React.MouseEvent, n: number) {
    if (!commentsEnabled || e.button !== 0) return
    e.preventDefault()
    setSel((prev) =>
      e.shiftKey && prev ? { anchor: prev.anchor, head: n } : { anchor: n, head: n }
    )
    setDragging(true)
  }

  function onNumMouseEnter(n: number) {
    if (!dragging) return
    setSel((prev) => (prev ? { anchor: prev.anchor, head: n } : { anchor: n, head: n }))
  }

  // Releasing on a gutter cell ends the drag immediately; the window listener
  // (above) is the fallback for a release anywhere else on the page.
  function onNumMouseUp() {
    setDragging(false)
  }

  // Build the row list flat: each code line, then any comment thread ending on
  // it, then the compose form if the selection ends there.
  const rows: ReactNode[] = []
  lines.forEach((nodes, i) => {
    const n = i + 1
    const lit = from != null && n >= from && n <= (to ?? from)
    const selected = sel != null && n >= selStart && n <= selEnd
    const cls = clsx("code-line", {
      "is-highlighted": lit,
      "is-selected": selected,
      "is-commented": commentedLines.has(n),
    })
    rows.push(
      <tr key={`L${n}`} id={`L${n}`} className={cls}>
        <td
          className={`code-line__num${commentsEnabled ? " is-clickable" : ""}`}
          data-line={n}
          onMouseDown={(e) => onNumMouseDown(e, n)}
          onMouseEnter={() => onNumMouseEnter(n)}
          onMouseUp={onNumMouseUp}
          title={commentsEnabled ? "Click or drag down the gutter to select lines" : undefined}
        />
        <td className="code-line__text">{nodes.length ? nodes : "\n"}</td>
      </tr>
    )

    const thread = byEndLine.get(n)
    if (thread && owner && repo) {
      rows.push(
        <tr key={`thread${n}`} className="code-comments-row">
          <td className="code-comments-cell" colSpan={2}>
            <ul className="code-comments">
              {thread.map((c) => (
                <CodeCommentCard
                  key={c.id}
                  owner={owner}
                  repo={repo}
                  comment={c}
                  canManage={!!currentUser && currentUser === c.author}
                />
              ))}
            </ul>
          </td>
        </tr>
      )
    }

    if (commentsEnabled && sel && !dragging && selEnd === n && owner && repo && path) {
      rows.push(
        <tr key={`compose${n}`} className="code-comments-row">
          <td className="code-comments-cell" colSpan={2}>
            <ComposeForm
              owner={owner}
              repo={repo}
              codeRef={codeRef}
              path={path}
              startLine={selStart}
              endLine={selEnd}
              onDone={() => setSel(null)}
            />
          </td>
        </tr>
      )
    }
  })

  return (
    <div className={`code-view hljs${dragging ? " is-selecting" : ""}`}>
      <table ref={tableRef} className="code-view__table">
        <tbody>{rows}</tbody>
      </table>
    </div>
  )
}

function CodeCommentCard({
  owner,
  repo,
  comment,
  canManage,
}: {
  owner: string
  repo: string
  comment: CodeComment
  canManage: boolean
}) {
  const resolve = useSetCodeCommentResolved(owner, repo)
  const del = useDeleteCodeComment(owner, repo)
  const span =
    comment.end_line > comment.start_line
      ? `${comment.start_line}–${comment.end_line}`
      : `${comment.start_line}`

  return (
    <li className={`comment${comment.resolved ? " is-resolved" : ""}`}>
      <span className="comment__avatar">
        <Avatar name={comment.author} />
      </span>
      <div className="comment__card">
        <div className="comment__head">
          <strong>{comment.author}</strong>
          <span className="muted">
            on lines {span} · {new Date(comment.created_at).toLocaleString()}
          </span>
          {comment.resolved && <span className="badge badge--done">resolved</span>}
          {canManage && (
            <span className="comment__actions">
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
                title="Delete comment"
                aria-label="Delete comment"
              >
                {del.isPending ? "…" : "×"}
              </button>
            </span>
          )}
        </div>
        <div className="comment__body">
          <Suspense fallback={<div className="markdown-body loading">Loading…</div>}>
            <Markdown content={comment.body} owner={owner} repo={repo} basePath="" />
          </Suspense>
        </div>
        {(resolve.error || del.error) && (
          <div className="error inline">{((resolve.error || del.error) as Error).message}</div>
        )}
      </div>
    </li>
  )
}

function ComposeForm({
  owner,
  repo,
  codeRef,
  path,
  startLine,
  endLine,
  onDone,
}: {
  owner: string
  repo: string
  codeRef: string
  path: string
  startLine: number
  endLine: number
  onDone: () => void
}) {
  const [body, setBody] = useState("")
  const create = useCreateCodeComment(owner, repo)
  const range = endLine > startLine ? `lines ${startLine}–${endLine}` : `line ${startLine}`

  function submit(e: React.SyntheticEvent) {
    e.preventDefault()
    const trimmed = body.trim()
    if (!trimmed || create.isPending) return
    create.mutate(
      { ref: codeRef, path, start_line: startLine, end_line: endLine, body: trimmed },
      {
        onSuccess: () => {
          setBody("")
          onDone()
        },
      }
    )
  }

  // Ctrl/Cmd+Enter posts the comment; plain Enter still inserts a newline.
  function onKeyDown(e: React.KeyboardEvent) {
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) submit(e)
  }

  return (
    <form className="comment-form code-compose" onSubmit={submit}>
      <div className="code-compose__head muted small">Commenting on {range}</div>
      <textarea
        className="textarea"
        placeholder="Leave a comment on this code"
        value={body}
        onChange={(e) => setBody(e.target.value)}
        onKeyDown={onKeyDown}
        rows={3}
      />
      {create.error && <div className="error">{(create.error as Error).message}</div>}
      <div className="row">
        <button
          type="submit"
          className="btn btn--primary"
          disabled={!body.trim() || create.isPending}
        >
          {create.isPending ? "Adding…" : "Add comment"}
        </button>
        <button type="button" className="btn btn--ghost" onClick={onDone}>
          Cancel
        </button>
      </div>
    </form>
  )
}

/**
 * Parse a `#L12` or `#L12-L34` (also tolerates `#L12-34`) URL hash into a
 * 1-based line range. Returns [null, null] when the hash isn't a line anchor.
 */
function parseLineRange(hash: string): [number | null, number | null] {
  const m = /^#L(\d+)(?:-L?(\d+))?$/.exec(hash)
  if (!m) return [null, null]
  return [parseInt(m[1], 10), m[2] ? parseInt(m[2], 10) : null]
}
