import { useEffect, useMemo, useRef } from "react"
import { useLocation } from "react-router-dom"
import { highlight, langFromPath, splitLines } from "../lib/highlight"

interface Props {
  content: string
  /** Repo-relative path of the file; used only to pick a highlight grammar. */
  path?: string
}

/**
 * Line-numbered source viewer in GitHub's blob style with lightweight syntax
 * highlighting. The whole file is tokenised once (so multi-line tokens such as
 * block comments stay correct), then the token tree is sliced into per-line
 * fragments to keep the sticky line-number gutter. Token colours come from the
 * Primer palette in styles.css — no extra theme stylesheet is loaded.
 *
 * A trailing newline is dropped so we don't show a phantom last line.
 *
 * Each row carries an `Ln` id so a `#L<n>` (or `#L<start>-L<end>`) URL hash —
 * e.g. a deep link from a Research suggested-read — scrolls the block into view
 * and highlights it, the way a code browser opens a file at a line.
 */
export default function CodeView({ content, path }: Props) {
  const lines = useMemo(() => {
    const body = content.endsWith("\n") ? content.slice(0, -1) : content
    return splitLines(highlight(body, path ? langFromPath(path) : undefined))
  }, [content, path])

  const { hash } = useLocation()
  const [from, to] = parseLineRange(hash)
  const tableRef = useRef<HTMLTableElement>(null)

  // Center the target line once the highlighted file has rendered. Depends on
  // `lines` too so it re-runs after a fresh file (e.g. opened in a new tab)
  // finishes tokenising.
  useEffect(() => {
    if (from == null) return
    tableRef.current
      ?.querySelector<HTMLElement>(`#L${from}`)
      ?.scrollIntoView({ block: "center" })
  }, [from, lines])

  return (
    <div className="code-view hljs">
      <table ref={tableRef} className="code-view__table">
        <tbody>
          {lines.map((nodes, i) => {
            const n = i + 1
            const lit = from != null && n >= from && n <= (to ?? from)
            return (
              <tr key={i} id={`L${n}`} className={`code-line${lit ? " is-highlighted" : ""}`}>
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

/**
 * Parse a `#L12` or `#L12-L34` (also tolerates `#L12-34`) URL hash into a
 * 1-based line range. Returns [null, null] when the hash isn't a line anchor.
 */
function parseLineRange(hash: string): [number | null, number | null] {
  const m = /^#L(\d+)(?:-L?(\d+))?$/.exec(hash)
  if (!m) return [null, null]
  return [parseInt(m[1], 10), m[2] ? parseInt(m[2], 10) : null]
}
