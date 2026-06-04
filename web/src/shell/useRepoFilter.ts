import { useSearchParams } from "react-router-dom"

// Syncs a repo multi-select with the URL (?repo=owner%2Fname,...).
// Selecting none = show all. Used by global list pages to narrow by repo
// client-side without a server round-trip.
export function useRepoFilter() {
  const [searchParams, setSearchParams] = useSearchParams()
  const raw = searchParams.get("repo") ?? ""
  const activeRepos = raw ? raw.split(",").filter(Boolean) : []

  function toggleRepo(repo: string) {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        const current = (prev.get("repo") ?? "").split(",").filter(Boolean)
        const updated = current.includes(repo)
          ? current.filter((r) => r !== repo)
          : [...current, repo]
        if (updated.length > 0) next.set("repo", updated.join(","))
        else next.delete("repo")
        return next
      },
      { replace: true }
    )
  }

  return { activeRepos, toggleRepo }
}
