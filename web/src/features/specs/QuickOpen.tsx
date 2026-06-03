import { useEffect, useMemo, useRef, useState } from "react"
import type { SpecListItem } from "@/api/types"

interface Props {
  open: boolean
  specs: SpecListItem[]
  onSelect: (path: string) => void
  onClose: () => void
}

/**
 * Sublime-style quick-open over the repo's specs: a borderless palette (opened
 * with ⌘P / Ctrl-P from the Specs tab) that fuzzy-matches a spec by title, id,
 * or path and opens it in the center pane. Keyboard-first — ↑/↓ move, Enter
 * opens, Esc dismisses. Built on the native <dialog> so focus trapping, Esc,
 * and the backdrop come for free.
 */
export default function QuickOpen({ open, specs, onSelect, onClose }: Props) {
  const dialogRef = useRef<HTMLDialogElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const [query, setQuery] = useState("")
  const [active, setActive] = useState(0)

  const matches = useMemo(() => rankSpecs(specs, query), [specs, query])

  // Drive the native dialog from the `open` prop; reset + focus on each open.
  useEffect(() => {
    const d = dialogRef.current
    if (!d) return
    if (open && !d.open) {
      setQuery("")
      setActive(0)
      d.showModal()
      inputRef.current?.focus()
    } else if (!open && d.open) {
      d.close()
    }
  }, [open])

  // A new query resets the highlight to the top result.
  // biome-ignore lint/correctness/useExhaustiveDependencies: reset highlight whenever the query changes
  useEffect(() => {
    setActive(0)
  }, [query])

  // Keep the highlighted row in view as ↑/↓ move it.
  useEffect(() => {
    const items = dialogRef.current?.querySelectorAll<HTMLElement>(".quickopen__item")
    items?.[active]?.scrollIntoView({ block: "nearest" })
  }, [active])

  function choose(path: string) {
    onSelect(path)
    onClose()
  }

  function onKeyDown(e: React.KeyboardEvent) {
    if (e.key === "ArrowDown") {
      e.preventDefault()
      setActive((i) => Math.min(i + 1, matches.length - 1))
    } else if (e.key === "ArrowUp") {
      e.preventDefault()
      setActive((i) => Math.max(i - 1, 0))
    } else if (e.key === "Enter") {
      e.preventDefault()
      const m = matches[active]
      if (m) choose(m.path)
    }
  }

  function onBackdropClick(e: React.MouseEvent<HTMLDialogElement>) {
    if (e.target === e.currentTarget) onClose()
  }

  return (
    // biome-ignore lint/a11y/useKeyWithClickEvents: backdrop click-to-dismiss only; <dialog> handles Esc/keyboard natively
    <dialog ref={dialogRef} className="quickopen" onClose={onClose} onClick={onBackdropClick}>
      <div className="quickopen__panel">
        <input
          ref={inputRef}
          className="quickopen__input"
          type="text"
          placeholder="Jump to a spec…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={onKeyDown}
          aria-label="Jump to a spec"
        />
        <ul className="quickopen__list">
          {matches.length === 0 ? (
            <li className="quickopen__empty muted small">No matching specs</li>
          ) : (
            matches.map((m, i) => (
              <li key={m.path}>
                <button
                  type="button"
                  className={`quickopen__item${i === active ? " is-active" : ""}`}
                  data-active={i === active || undefined}
                  onMouseEnter={() => setActive(i)}
                  onClick={() => choose(m.path)}
                >
                  <span className="quickopen__title">{m.title}</span>
                  <span className="quickopen__path muted small">{m.path}</span>
                </button>
              </li>
            ))
          )}
        </ul>
      </div>
    </dialog>
  )
}

// rankSpecs returns the specs that fuzzy-match the query, best first. An empty
// query keeps the list as-is (the palette opens showing everything).
function rankSpecs(specs: SpecListItem[], query: string): SpecListItem[] {
  const q = query.trim()
  if (!q) return specs
  const scored: Array<{ spec: SpecListItem; score: number }> = []
  for (const spec of specs) {
    const score = bestScore(q, [spec.title, spec.id, spec.path])
    if (score !== null) scored.push({ spec, score })
  }
  scored.sort((a, b) => a.score - b.score || a.spec.path.localeCompare(b.spec.path))
  return scored.map((s) => s.spec)
}

// bestScore is the lowest (best) fuzzy score of the query against any of the
// candidate strings, or null when it matches none.
function bestScore(query: string, candidates: string[]): number | null {
  let best: number | null = null
  for (const c of candidates) {
    const s = fuzzyScore(query, c)
    if (s !== null && (best === null || s < best)) best = s
  }
  return best
}

// fuzzyScore subsequence-matches query against text (case-insensitive) and
// scores by how tight and early the match is — lower is better. Returns null
// when text doesn't contain the query as a subsequence.
function fuzzyScore(query: string, text: string): number | null {
  const q = query.toLowerCase()
  const t = text.toLowerCase()
  let qi = 0
  let score = 0
  let last = -1
  for (let ti = 0; ti < t.length && qi < q.length; ti++) {
    if (t[ti] === q[qi]) {
      score += last === -1 ? ti : ti - last - 1 // leading offset, then gaps
      last = ti
      qi++
    }
  }
  return qi === q.length ? score : null
}
