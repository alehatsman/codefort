import clsx from "clsx"
import { useMemo } from "react"
import { highlight, langFromPath, splitLines } from "@/ui/highlight"

interface Props {
  /** Source text of the region to render (may be a slice of a larger file). */
  code: string
  /** Repo-relative path; picks the highlight grammar. Omit for auto-detect. */
  path?: string
  /** 1-based line number of the region's first line, so the gutter matches the
   *  original file rather than restarting at 1. Defaults to 1. */
  startLine?: number
  /** Absolute (file-relative) 1-based [start, end] range, inclusive, to tint as
   *  the focus of this region — e.g. the lines a review comment annotates. */
  focus?: [number, number]
  className?: string
}

/**
 * Read-only, line-numbered source block in the blob viewer's style: a region of
 * code with an optional tinted focus range. Domain-agnostic — the caller slices
 * the lines, sets the starting line number, and says which lines to highlight.
 *
 * The interactive blob viewer (CodeView) keeps its own clickable gutter; this is
 * the static "cut from the code view" rendering used by the review list, where
 * each comment shows its annotated lines with a few lines of context.
 */
export default function CodeSnippet({ code, path, startLine = 1, focus, className }: Props) {
  const lines = useMemo(() => {
    const body = code.endsWith("\n") ? code.slice(0, -1) : code
    return splitLines(highlight(body, path ? langFromPath(path) : undefined))
  }, [code, path])

  const [from, to] = focus ?? [0, 0]

  return (
    <div className={clsx("code-snippet hljs", className)}>
      <table className="code-snippet__table">
        <tbody>
          {lines.map((nodes, i) => {
            const n = startLine + i
            return (
              <tr key={n} className={clsx("code-line", { "is-focus": n >= from && n <= to })}>
                <td className="code-line__num" data-line={n} />
                <td className="code-line__text">{nodes.length ? nodes : "\n"}</td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
