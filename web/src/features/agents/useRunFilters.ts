import { useEffect, useState } from "react"
import { useSearchParams } from "react-router-dom"
import type { CIRunStatus } from "@/api/types"

// useRunFilters owns the Agents views' fulltext + status filter state, mirroring
// the issues pages: the committed search term lives in the URL ?q (debounced
// 250ms so typing doesn't spam the API), while the status chips are local
// component state. It returns `query` — the ?state=&q= string to hand to
// useAllRuns / useCIRuns (kind is added by the caller).
export function useRunFilters() {
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

  const params = new URLSearchParams()
  if (activeStates.length > 0) params.set("state", activeStates.join(","))
  if (committedQuery) params.set("q", committedQuery)

  return { search, setSearch, activeStates, toggleState, query: params.toString() }
}
