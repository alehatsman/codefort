import { useMemo } from "react"
import type { ReactNode } from "react"
import type { DiffFile, DiffHunk, DiffLine } from "../api/types"
import { highlightLine, langFromPath } from "../lib/highlight"

interface Props {
  file: DiffFile
  /** "split" = side-by-side (default), "unified" = single column. */
  mode: "split" | "unified"
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
 */
export default function DiffView({ file, mode }: Props) {
  // Grammar is picked from the post-image path (or the old path for a delete).
  const lang = useMemo(
    () => langFromPath(file.new_path || file.old_path),
    [file.new_path, file.old_path]
  )

  const title =
    file.status === "renamed"
      ? `${file.old_path} → ${file.new_path}`
      : file.new_path || file.old_path

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
        <SplitTable hunks={file.hunks} lang={lang} />
      ) : (
        <UnifiedTable hunks={file.hunks} lang={lang} />
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

function SplitTable({ hunks, lang }: { hunks: DiffHunk[]; lang?: string }) {
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
      <tbody>
        {hunks.map((h, hi) => (
          <HunkSplit key={hi} hunk={h} lang={lang} />
        ))}
      </tbody>
    </table>
  )
}

function HunkSplit({ hunk, lang }: { hunk: DiffHunk; lang?: string }) {
  const rows = pairRows(hunk.lines)
  return (
    <>
      <tr className="diff-hunk">
        <td colSpan={4} className="diff-hunk__header">
          {hunk.header || "…"}
        </td>
      </tr>
      {rows.map((r, i) => (
        <tr key={i} className="diff-row">
          <td className="diff-num" data-line={r.left ? r.left.old : ""} />
          <td className={`diff-code diff-code--${r.left ? r.left.kind : "empty"}`}>
            {r.left ? cell(r.left.text, lang) : null}
          </td>
          <td className="diff-num" data-line={r.right ? r.right.new : ""} />
          <td className={`diff-code diff-code--${r.right ? r.right.kind : "empty"}`}>
            {r.right ? cell(r.right.text, lang) : null}
          </td>
        </tr>
      ))}
    </>
  )
}

function UnifiedTable({ hunks, lang }: { hunks: DiffHunk[]; lang?: string }) {
  return (
    <table className="diff-unified hljs">
      <colgroup>
        <col className="diff-col-num" />
        <col className="diff-col-num" />
        <col className="diff-col-code" />
      </colgroup>
      <tbody>
        {hunks.map((h, hi) => (
          <HunkUnified key={hi} hunk={h} lang={lang} />
        ))}
      </tbody>
    </table>
  )
}

function HunkUnified({ hunk, lang }: { hunk: DiffHunk; lang?: string }) {
  return (
    <>
      <tr className="diff-hunk">
        <td colSpan={3} className="diff-hunk__header">
          {hunk.header || "…"}
        </td>
      </tr>
      {hunk.lines.map((l, i) => (
        <tr key={i} className="diff-row">
          <td className="diff-num" data-line={l.old || ""} />
          <td className="diff-num" data-line={l.new || ""} />
          <td className={`diff-code diff-code--${l.kind}`}>
            <span className="diff-marker">
              {l.kind === "add" ? "+" : l.kind === "del" ? "−" : " "}
            </span>
            {cell(l.text, lang)}
          </td>
        </tr>
      ))}
    </>
  )
}
