import type { ReactNode } from "react"
import { common, createLowlight } from "lowlight"
import type { Element, Root, RootContent } from "hast"

// One highlighter for the whole app. `common` covers ~37 popular languages
// (Go, TS/JS, Python, Rust, JSON, YAML, shell, …) without the bundle weight of
// the full grammar set. Shared by the blob viewer (CodeView) and markdown
// code fences (Markdown) so only one grammar set is loaded.
const lowlight = createLowlight(common)

/**
 * Tokenise source into a hast tree. Uses the given grammar when known,
 * otherwise falls back to language auto-detection. Tokenising the whole input
 * at once keeps multi-line tokens (block comments, template strings) correct.
 */
export function highlight(content: string, lang?: string): Root {
  return lang && lowlight.registered(lang)
    ? lowlight.highlight(lang, content)
    : lowlight.highlightAuto(content)
}

/**
 * Flatten a hast tree into an array of lines, where each line is a list of
 * React nodes. Text is split on "\n"; each run inherits the class of its
 * nearest ancestor element so colouring matches what highlight.js intends.
 * Used by the line-numbered blob viewer.
 */
export function splitLines(tree: Root): ReactNode[][] {
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

  walk(tree.children, push)
  return lines
}

/**
 * Flatten a hast tree into a single list of React nodes, preserving newlines
 * as text. Used for code fences inside rendered markdown, which want the
 * highlighting but not the line-number gutter.
 */
export function highlightNodes(tree: Root): ReactNode[] {
  const nodes: ReactNode[] = []
  let key = 0
  walk(tree.children, (text, className) => {
    if (text === "") return
    nodes.push(
      className ? (
        <span key={key++} className={className}>
          {text}
        </span>
      ) : (
        text
      ),
    )
  })
  return nodes
}

// walk emits each text run with the class of its nearest ancestor element.
function walk(
  nodes: RootContent[],
  emit: (text: string, className?: string) => void,
  className?: string,
) {
  for (const node of nodes) {
    if (node.type === "text") {
      emit(node.value, className)
    } else if (node.type === "element") {
      walk(node.children, emit, classNameOf(node) ?? className)
    }
  }
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

export function langFromPath(path: string): string | undefined {
  const file = path.split("/").pop() ?? ""
  const lower = file.toLowerCase()
  if (NAME_LANG[lower]) return NAME_LANG[lower]
  const dot = lower.lastIndexOf(".")
  if (dot < 0) return undefined
  return EXT_LANG[lower.slice(dot + 1)]
}
