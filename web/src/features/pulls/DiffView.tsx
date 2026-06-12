import clsx from "clsx"
import { useMemo, useRef, useState } from "react"
import type { ReactNode } from "react"
import type { CodeComment, DiffFile, DiffHunk, DiffLine } from "@/api/types"
import { Button } from "@/ui"
import { highlightLine, langFromPath } from "@/ui/highlight"

interface Props {
  file: DiffFile
  /** "split" = side-by-side (default), "unified" = single column. */
  mode: "split" | "unified"
  comments?: CodeComment[]
  onAddComment?: (path: string, line: number, body: string) => Promise<void>
}

const STATUS_LABEL: Record<DiffFile["status"], string> = {
  added: "added",
  modified: "modified",
  deleted: "deleted",
  renamed: "renamed",
}

/**
 * One file's diff, rendered as a collapsible card. Split mode lays each hunk
 * out side-by-side (old left, new right); unified mode stacks them in a single
 * column. Per-line syntax highlighting reuses the shared lowlight grammar set
 * (see highlight.tsx) so colours match the blob viewer.
 *
 * When comments + onAddComment are provided, a "+" affordance appears on hover
 * over each changed line and existing comments thread inline after their anchor.
 */
export default function DiffView({ file, mode, comments = [], onAddComment }: Props) {
  const lang = useMemo(
    () => langFromPath(file.new_path || file.old_path),
    [file.new_path, file.old_path]
  )

  const title =
    file.status === "renamed"
      ? `${file.old_path} → ${file.new_path}`
      : file.new_path || file.old_path

  // Index comments by their anchor line number for O(1) lookup per row.
  const commentsByLine = useMemo(() => {
    const m = new Map<number, CodeComment[]>()
    for (const c of comments) {
      const lineNo = c.end_line || c.start_line
      const arr = m.get(lineNo) ?? []
      arr.push(c)
      m.set(lineNo, arr)
    }
    return m
  }, [comments])

  const filePath = file.new_path || file.old_path

  return (
    <details className="diff-file" open>
      <summary className="diff-file__head">
        <span className={`diff-file__status diff-file__status--${file.status}`}>
          {STATUS_LABEL[file.status]}
        </span>
        <span className="diff-file__path" title={title}>
          {title}
        </span>
        <span className="diff-file__counts">
          {file.additions > 0 && <span className="diff-file__add">+{file.additions}</span>}
          {file.deletions > 0 && <span className="diff-file__del">−{file.deletions}</span>}
        </span>
      </summary>

      {file.binary ? (
        <div className="diff-file__note">Binary file not shown.</div>
      ) : file.hunks.length === 0 ? (
        <div className="diff-file__note">No content changes.</div>
      ) : mode === "split" ? (
        <SplitTable
          hunks={file.hunks}
          lang={lang}
          path={filePath}
          commentsByLine={commentsByLine}
          onAddComment={onAddComment}
        />
      ) : (
        <UnifiedTable
          hunks={file.hunks}
          lang={lang}
          path={filePath}
          commentsByLine={commentsByLine}
          onAddComment={onAddComment}
        />
      )}
    </details>
  )
}

// cell renders a line's body, syntax-highlighted, preserving height for blanks.
function cell(text: string, lang?: string): ReactNode {
  const nodes = highlightLine(text, lang)
  return nodes.length ? nodes : "\n"
}

interface SplitRow {
  left: DiffLine | null
  right: DiffLine | null
}

// pairRows turns a hunk's line sequence into side-by-side rows: a run of
// deletions is paired index-wise with the run of additions that follows it
// (leftovers fall to one side), and context lines align on both sides.
function pairRows(lines: DiffLine[]): SplitRow[] {
  const rows: SplitRow[] = []
  let dels: DiffLine[] = []
  let adds: DiffLine[] = []

  const flush = () => {
    const n = Math.max(dels.length, adds.length)
    for (let i = 0; i < n; i++) {
      rows.push({ left: dels[i] ?? null, right: adds[i] ?? null })
    }
    dels = []
    adds = []
  }

  for (const l of lines) {
    if (l.kind === "del") dels.push(l)
    else if (l.kind === "add") adds.push(l)
    else {
      flush()
      rows.push({ left: l, right: l })
    }
  }
  flush()
  return rows
}

interface TableProps {
  hunks: DiffHunk[]
  lang?: string
  path: string
  commentsByLine: Map<number, CodeComment[]>
  onAddComment?: (path: string, line: number, body: string) => Promise<void>
}

// InlineCommentForm renders an open textarea + cancel/submit when the user
// clicked "+" on a diff line.
function InlineCommentForm({
  path,
  lineNo,
  colSpan,
  onSubmit,
  onCancel,
}: {
  path: string
  lineNo: number
  colSpan: number
  onSubmit: (path: string, line: number, body: string) => Promise<void>
  onCancel: () => void
}) {
  const [body, setBody] = useState("")
  const [saving, setSaving] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    if (!body.trim()) return
    setSaving(true)
    try {
      await onSubmit(path, lineNo, body.trim())
      setBody("")
    } finally {
      setSaving(false)
    }
  }

  return (
    <tr className="diff-comment-row">
      <td colSpan={colSpan} className="diff-comment-cell">
        <form className="diff-comment-form" onSubmit={submit}>
          <textarea
            className="diff-comment-form__input textarea"
            placeholder="Leave a comment…"
            value={body}
            onChange={(e) => setBody(e.target.value)}
            rows={3}
            // biome-ignore lint/a11y/noAutofocus: user just clicked "+" to open this form
            autoFocus
          />
          <div className="diff-comment-form__actions">
            <Button type="button" variant="ghost" size="small" onClick={onCancel}>
              Cancel
            </Button>
            <Button type="submit" variant="primary" size="small" disabled={!body.trim() || saving}>
              {saving ? "Saving…" : "Comment"}
            </Button>
          </div>
        </form>
      </td>
    </tr>
  )
}

// InlineCommentThread renders existing comments anchored to a line.
function InlineCommentThread({ comments, colSpan }: { comments: CodeComment[]; colSpan: number }) {
  if (comments.length === 0) return null
  return (
    <tr className="diff-comment-row">
      <td colSpan={colSpan} className="diff-comment-cell">
        {comments.map((c) => (
          <div key={c.id} className={clsx("diff-inline-comment", { "is-resolved": c.resolved })}>
            <span className="diff-inline-comment__author">{c.author}</span>
            <span className="diff-inline-comment__body">{c.body}</span>
            {c.resolved && (
              <span className="diff-inline-comment__resolved muted small">resolved</span>
            )}
          </div>
        ))}
      </td>
    </tr>
  )
}

function SplitTable({ hunks, lang, path, commentsByLine, onAddComment }: TableProps) {
  return (
    <table className="diff-split hljs">
      {/* Fixed layout sizes columns from <col>, not later-row cells: keep the
          two number columns narrow so each code column splits the rest. */}
      <colgroup>
        <col className="diff-col-num" />
        <col className="diff-col-code" />
        <col className="diff-col-num" />
        <col className="diff-col-code" />
      </colgroup>
      {hunks.map((h) => (
        <HunkSplit
          key={h.header}
          hunk={h}
          lang={lang}
          path={path}
          commentsByLine={commentsByLine}
          onAddComment={onAddComment}
        />
      ))}
    </table>
  )
}

function HunkSplit({
  hunk,
  lang,
  path,
  commentsByLine,
  onAddComment,
}: {
  hunk: DiffHunk
  lang?: string
  path: string
  commentsByLine: Map<number, CodeComment[]>
  onAddComment?: (path: string, line: number, body: string) => Promise<void>
}) {
  const rows = pairRows(hunk.lines)
  const [openLine, setOpenLine] = useState<number | null>(null)
  const tableRef = useRef<HTMLTableSectionElement>(null)

  return (
    <tbody ref={tableRef}>
      <tr className="diff-hunk">
        <td colSpan={4} className="diff-hunk__header">
          {hunk.header || "…"}
        </td>
      </tr>
      {rows.flatMap((r) => {
        const lineNo = r.right?.new ?? r.left?.old ?? 0
        const isChanged = r.left?.kind !== "context" || r.right?.kind !== "context"
        const rowComments = commentsByLine.get(lineNo) ?? []

        const rowEl = (
          <tr
            key={`${r.left?.old ?? "_"}:${r.right?.new ?? "_"}`}
            className={clsx("diff-row", { "has-comments": rowComments.length > 0 })}
          >
            <td className="diff-num" data-line={r.left ? r.left.old : ""} />
            <td className={`diff-code diff-code--${r.left ? r.left.kind : "empty"}`}>
              {r.left ? cell(r.left.text, lang) : null}
            </td>
            <td className="diff-num" data-line={r.right ? r.right.new : ""} />
            <td className={`diff-code diff-code--${r.right ? r.right.kind : "empty"}`}>
              {r.right ? cell(r.right.text, lang) : null}
              {onAddComment && isChanged && lineNo > 0 && (
                <button
                  type="button"
                  className="diff-add-comment"
                  onClick={() => setOpenLine((prev) => (prev === lineNo ? null : lineNo))}
                  aria-label="Add inline comment"
                >
                  +
                </button>
              )}
            </td>
          </tr>
        )

        const extra: ReactNode[] = []
        if (rowComments.length > 0) {
          extra.push(
            <InlineCommentThread key={`thread-${lineNo}`} comments={rowComments} colSpan={4} />
          )
        }
        if (onAddComment && openLine === lineNo) {
          extra.push(
            <InlineCommentForm
              key={`form-${lineNo}`}
              path={path}
              lineNo={lineNo}
              colSpan={4}
              onSubmit={async (p, l, b) => {
                await onAddComment(p, l, b)
                setOpenLine(null)
              }}
              onCancel={() => setOpenLine(null)}
            />
          )
        }

        return extra.length > 0 ? [rowEl, ...extra] : rowEl
      })}
    </tbody>
  )
}

function UnifiedTable({ hunks, lang, path, commentsByLine, onAddComment }: TableProps) {
  return (
    <table className="diff-unified hljs">
      <colgroup>
        <col className="diff-col-num" />
        <col className="diff-col-num" />
        <col className="diff-col-code" />
      </colgroup>
      <tbody>
        {hunks.map((h) => (
          <HunkUnified
            key={h.header}
            hunk={h}
            lang={lang}
            path={path}
            commentsByLine={commentsByLine}
            onAddComment={onAddComment}
          />
        ))}
      </tbody>
    </table>
  )
}

function HunkUnified({
  hunk,
  lang,
  path,
  commentsByLine,
  onAddComment,
}: {
  hunk: DiffHunk
  lang?: string
  path: string
  commentsByLine: Map<number, CodeComment[]>
  onAddComment?: (path: string, line: number, body: string) => Promise<void>
}) {
  const [openLine, setOpenLine] = useState<number | null>(null)

  return (
    <>
      <tr className="diff-hunk">
        <td colSpan={3} className="diff-hunk__header">
          {hunk.header || "…"}
        </td>
      </tr>
      {hunk.lines.flatMap((l) => {
        const lineNo = l.new || l.old || 0
        const isChanged = l.kind !== "context"
        const rowComments = commentsByLine.get(lineNo) ?? []

        const rowEl = (
          <tr
            key={`${l.old}:${l.new}`}
            className={clsx("diff-row", { "has-comments": rowComments.length > 0 })}
          >
            <td className="diff-num" data-line={l.old || ""} />
            <td className="diff-num" data-line={l.new || ""} />
            <td className={`diff-code diff-code--${l.kind}`}>
              <span className="diff-marker">
                {l.kind === "add" ? "+" : l.kind === "del" ? "−" : " "}
              </span>
              {cell(l.text, lang)}
              {onAddComment && isChanged && lineNo > 0 && (
                <button
                  type="button"
                  className="diff-add-comment"
                  onClick={() => setOpenLine((prev) => (prev === lineNo ? null : lineNo))}
                  aria-label="Add inline comment"
                >
                  +
                </button>
              )}
            </td>
          </tr>
        )

        const extra: ReactNode[] = []
        if (rowComments.length > 0) {
          extra.push(
            <InlineCommentThread key={`thread-${lineNo}`} comments={rowComments} colSpan={3} />
          )
        }
        if (onAddComment && openLine === lineNo) {
          extra.push(
            <InlineCommentForm
              key={`form-${lineNo}`}
              path={path}
              lineNo={lineNo}
              colSpan={3}
              onSubmit={async (p, ln, b) => {
                await onAddComment(p, ln, b)
                setOpenLine(null)
              }}
              onCancel={() => setOpenLine(null)}
            />
          )
        }

        return extra.length > 0 ? [rowEl, ...extra] : rowEl
      })}
    </>
  )
}
