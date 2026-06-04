import { useEffect, useState } from "react"
import { useSearchParams } from "react-router-dom"
import type { CIRunStatus } from "@/api/types"
import type { RunChip } from "./runChips"

// useRunFilters owns the Agents/Pipelines fulltext + status filter state,
// mirroring the issues pages: the committed search term lives in the URL ?q
// (debounced 250ms so typing doesn't spam the API), while the status chips are
// local component state. It returns `query` — the ?state=&q= string to hand to
// useAllRuns / useCIRuns (kind is added by the caller).
//
// Pass the chip definitions from AGENT_CHIPS or CI_RUN_CHIPS so the hook can
// expand each chip key to the backend statuses it covers when building ?state=.
export function useRunFilters(chips: readonly RunChip[]) {
  const [activeStates, setActiveStates] = useState<CIRunStatus[]>([])

  const [searchParams, setSearchParams] = useSearchParams()
  const committedQuery = searchParams.get("q") ?? ""
  const [search, setSearch] = useState(committedQuery)

  useEffect(() => {
    const trimmed = search.trim()
    if (trimmed === committedQuery) return
    const t = setTimeout(() => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          if (trimmed) next.set("q", trimmed)
          else next.delete("q")
          return next
        },
        { replace: true }
      )
    }, 250)
    return () => clearTimeout(t)
  }, [search, committedQuery, setSearchParams])

  function toggleState(s: CIRunStatus) {
    setActiveStates((prev) => (prev.includes(s) ? prev.filter((x) => x !== s) : [...prev, s]))
  }

  // Expand each active chip key to the backend statuses it covers, deduped.
  const chipMap = new Map(chips.map((c) => [c.key, c.statuses]))
  const expanded = [...new Set(activeStates.flatMap((s) => chipMap.get(s) ?? [s]))]

  const params = new URLSearchParams()
  if (expanded.length > 0) params.set("state", expanded.join(","))
  if (committedQuery) params.set("q", committedQuery)

  return { search, setSearch, activeStates, toggleState, query: params.toString() }
}
