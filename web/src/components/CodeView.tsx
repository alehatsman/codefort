interface Props {
  content: string
}

/**
 * Line-numbered source viewer in GitHub's blob style. No syntax
 * highlighting — a plain, fast monospace render with a sticky line-number
 * gutter. A trailing newline is dropped so we don't show a phantom last
 * line.
 */
export default function CodeView({ content }: Props) {
  const body = content.endsWith("\n") ? content.slice(0, -1) : content
  const lines = body.split("\n")

  return (
    <div className="code-view">
      <table className="code-view__table">
        <tbody>
          {lines.map((line, i) => (
            <tr key={i} className="code-line">
              <td className="code-line__num" data-line={i + 1} />
              <td className="code-line__text">{line === "" ? "\n" : line}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
