import { useEffect, useRef, useState } from "react"
import { useLocation, useNavigate } from "react-router-dom"

/**
 * Vim-style keyboard navigation shared across the repos, code, and issues
 * views. Two hooks: `useListNav` drives a roving selection over a list
 * (j/k + Enter), `useTabNav` switches between the repo tabs (h/l). Both
 * stay out of the way while the user is typing in a field.
 */

// Don't hijack keys while the user is editing text or working a control.
function isEditableTarget(): boolean {
  const el = document.activeElement as HTMLElement | null
  if (!el) return false
  const tag = el.tagName
  return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || el.isContentEditable
}

interface ListNavOptions {
  count: number
  onActivate: (index: number) => void
  // Grid layouts pass a column-count getter (read live, since columns reflow
  // with viewport width). When set, j/k move a whole row (±columns) and h/l
  // move one cell; for a plain list (the default) j/k step by one and h/l are
  // left for `useTabNav`.
  getColumns?: () => number
  enabled?: boolean
}

/**
 * Roving selection over a list of `count` items. Selection starts unset
 * (index -1) so nothing is highlighted until the user first presses a nav
 * key. Enter activates the current item. Returns the selected index; the
 * caller marks the matching element `data-vim-selected="true"` so it can be
 * scrolled into view.
 */
export function useListNav({ count, onActivate, getColumns, enabled = true }: ListNavOptions) {
  const [index, setIndex] = useState(-1)
  const activateRef = useRef(onActivate)
  activateRef.current = onActivate
  const columnsRef = useRef(getColumns)
  columnsRef.current = getColumns
  const indexRef = useRef(index)
  indexRef.current = index

  // Keep the selection in range as the list grows or shrinks (filtering).
  useEffect(() => {
    setIndex((i) => (i >= count ? count - 1 : i))
  }, [count])

  useEffect(() => {
    if (!enabled || count === 0) return
    function onKey(e: KeyboardEvent) {
      if (isEditableTarget() || e.metaKey || e.ctrlKey || e.altKey) return
      // In grid mode j/k jump a row; in list mode they step by one. h/l only
      // navigate in grid mode (a list leaves them to the tab switcher).
      const grid = !!columnsRef.current
      const cols = grid ? Math.max(1, Math.round(columnsRef.current!())) : 1
      let delta: number | null = null
      if (e.key === "j" || e.key === "ArrowDown") delta = grid ? cols : 1
      else if (e.key === "k" || e.key === "ArrowUp") delta = grid ? -cols : -1
      else if (grid && (e.key === "l" || e.key === "ArrowRight")) delta = 1
      else if (grid && (e.key === "h" || e.key === "ArrowLeft")) delta = -1
      else if (e.key === "Enter") {
        const i = indexRef.current
        if (i >= 0 && i < count) {
          e.preventDefault()
          activateRef.current(i)
        }
        return
      }
      if (delta === null) return
      e.preventDefault()
      const step = delta
      // First key just selects the top item; afterwards move and clamp so a
      // row/cell step never runs off either end of the list.
      setIndex((i) => {
        if (i < 0) return 0
        const target = i + step
        return target >= 0 && target < count ? target : i
      })
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [count, enabled])

  // Keep the selected row in view as it moves.
  useEffect(() => {
    if (index < 0) return
    document
      .querySelector<HTMLElement>("[data-vim-selected='true']")
      ?.scrollIntoView({ block: "nearest" })
  }, [index])

  return { index }
}

// Repo tabs, in the order h/l walk them. Suffixes append to `/owner/repo`.
const TAB_SUFFIXES = ["", "/issues", "/research", "/pipelines"] as const

/**
 * h/l (and ←/→) cycle between the repo tabs (Code / Issues / Research /
 * Pipelines). The active tab is derived from the URL, matching `RepoHeader`'s
 * own logic.
 */
export function useTabNav(owner: string, repo: string, enabled = true) {
  const navigate = useNavigate()
  const { pathname } = useLocation()
  const base = `/${owner}/${repo}`

  let current = 0
  if (pathname.startsWith(`${base}/issues`)) current = 1
  else if (pathname.startsWith(`${base}/research`)) current = 2
  else if (pathname.startsWith(`${base}/pipelines`)) current = 3
  const currentRef = useRef(current)
  currentRef.current = current

  useEffect(() => {
    if (!enabled || !owner || !repo) return
    function onKey(e: KeyboardEvent) {
      if (isEditableTarget() || e.metaKey || e.ctrlKey || e.altKey) return
      const right = e.key === "l" || e.key === "ArrowRight"
      const left = e.key === "h" || e.key === "ArrowLeft"
      if (!right && !left) return
      e.preventDefault()
      const i = currentRef.current
      const target = right ? Math.min(i + 1, TAB_SUFFIXES.length - 1) : Math.max(i - 1, 0)
      if (target !== i) navigate(`${base}${TAB_SUFFIXES[target]}`)
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [enabled, owner, repo, base, navigate])

  return current
}
