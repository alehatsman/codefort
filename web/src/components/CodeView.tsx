import { useMemo, type ReactNode } from "react"
import { common, createLowlight } from "lowlight"
import type { Element, Root, RootContent } from "hast"

interface Props {
  content: string
  /** Repo-relative path of the file; used only to pick a highlight grammar. */
  path?: string
}

// One highlighter for the whole app. `common` covers ~37 popular languages
// (Go, TS/JS, Python, Rust, JSON, YAML, shell, …) without the bundle weight of
// the full grammar set.
const lowlight = createLowlight(common)

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
    const lang = path ? langFromPath(path) : undefined
    const tree =
      lang && lowlight.registered(lang)
        ? lowlight.highlight(lang, body)
        : lowlight.highlightAuto(body)
    return splitLines(tree)
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

/**
 * Flatten a hast tree into an array of lines, where each line is a list of
 * React nodes. Text is split on "\n"; each run inherits the class of its
 * nearest ancestor element so colouring matches what highlight.js intends.
 */
function splitLines(tree: Root): ReactNode[][] {
  const lines: ReactNode[][] = [[]]
  let key = 0

  const push = (text: string, className?: string) => {
    const parts = text.split("\n")
    parts.forEach((part, i) => {
      if (i > 0) lines.push([])
      if (part === "") return
      const current = lines[lines.length - 1]
      current.push(
        className ? (
          <span key={key++} className={className}>
            {part}
          </span>
        ) : (
          part
        ),
      )
    })
  }

  const walk = (nodes: RootContent[], className?: string) => {
    for (const node of nodes) {
      if (node.type === "text") {
        push(node.value, className)
      } else if (node.type === "element") {
        walk(node.children, classNameOf(node) ?? className)
      }
    }
  }

  walk(tree.children)
  return lines
}

function classNameOf(el: Element): string | undefined {
  const cn = el.properties?.className
  if (Array.isArray(cn)) return cn.join(" ")
  if (typeof cn === "string") return cn
  return undefined
}

// Map file extensions / well-known filenames to highlight.js language ids.
const EXT_LANG: Record<string, string> = {
  go: "go",
  ts: "typescript",
  tsx: "typescript",
  js: "javascript",
  jsx: "javascript",
  mjs: "javascript",
  cjs: "javascript",
  json: "json",
  py: "python",
  rb: "ruby",
  rs: "rust",
  java: "java",
  kt: "kotlin",
  c: "c",
  h: "c",
  cc: "cpp",
  cpp: "cpp",
  hpp: "cpp",
  cs: "csharp",
  php: "php",
  swift: "swift",
  scala: "scala",
  sh: "bash",
  bash: "bash",
  zsh: "bash",
  yml: "yaml",
  yaml: "yaml",
  toml: "ini",
  ini: "ini",
  md: "markdown",
  markdown: "markdown",
  html: "xml",
  xml: "xml",
  css: "css",
  scss: "scss",
  less: "less",
  sql: "sql",
  dockerfile: "dockerfile",
  makefile: "makefile",
  diff: "diff",
  patch: "diff",
}

const NAME_LANG: Record<string, string> = {
  dockerfile: "dockerfile",
  makefile: "makefile",
  "go.mod": "ini",
  "go.sum": "ini",
}

function langFromPath(path: string): string | undefined {
  const file = path.split("/").pop() ?? ""
  const lower = file.toLowerCase()
  if (NAME_LANG[lower]) return NAME_LANG[lower]
  const dot = lower.lastIndexOf(".")
  if (dot < 0) return undefined
  return EXT_LANG[lower.slice(dot + 1)]
}
