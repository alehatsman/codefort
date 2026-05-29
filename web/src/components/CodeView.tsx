import { useMemo } from "react"
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
 */
export default function CodeView({ content, path }: Props) {
  const lines = useMemo(() => {
    const body = content.endsWith("\n") ? content.slice(0, -1) : content
    return splitLines(highlight(body, path ? langFromPath(path) : undefined))
  }, [content, path])

  return (
    <div className="code-view hljs">
      <table className="code-view__table">
        <tbody>
          {lines.map((nodes, i) => (
            <tr key={i} className="code-line">
              <td className="code-line__num" data-line={i + 1} />
              <td className="code-line__text">{nodes.length ? nodes : "\n"}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
