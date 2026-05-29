import type { TreeEntry } from "../api/types"

// Recognised README filenames (case-insensitive), mirroring GitHub.
const README_RE = /^readme(\.(md|markdown|mdown|mkdn|txt|rst))?$/i
const MARKDOWN_EXT = /\.(md|markdown|mdown|mkdn)$/i

/**
 * Pick the README to render from a directory listing, GitHub-style: markdown
 * variants win over plain text, which win over an extensionless README; ties
 * break case-insensitively by name. Returns undefined when none is present.
 */
export function findReadme(entries: TreeEntry[]): TreeEntry | undefined {
  const candidates = entries.filter((e) => e.type === "blob" && README_RE.test(e.name))
  if (candidates.length === 0) return undefined
  return candidates.sort((a, b) => rank(a.name) - rank(b.name) || cmp(a.name, b.name))[0]
}

/** True when a README should be rendered as markdown rather than plain text. */
export function isMarkdown(name: string): boolean {
  return MARKDOWN_EXT.test(name)
}

function rank(name: string): number {
  if (MARKDOWN_EXT.test(name)) return 0
  if (name.toLowerCase() === "readme") return 2 // extensionless: last resort
  return 1 // .txt, .rst, …
}

function cmp(a: string, b: string): number {
  return a.toLowerCase().localeCompare(b.toLowerCase())
}
