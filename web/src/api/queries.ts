import { useQuery, useQueryClient } from "@tanstack/react-query"
import { api } from "./client"
import type { Issue, Repo } from "./types"

// Query keys live in one place so mutations can invalidate consistently.
// Pattern: hierarchical arrays so `["issues", owner, repo]` invalidation
// also covers `["issues", owner, repo, "filter-query"]` automatically.
export const keys = {
  whoami: () => ["whoami"] as const,
  repos: () => ["repos"] as const,
  repo: (owner: string, repo: string) => ["repo", owner, repo] as const,
  tree: (owner: string, repo: string, path: string) => ["tree", owner, repo, path] as const,
  blob: (owner: string, repo: string, path: string) => ["blob", owner, repo, path] as const,
  issues: (owner: string, repo: string, query = "") =>
    query ? (["issues", owner, repo, query] as const) : (["issues", owner, repo] as const),
  issue: (owner: string, repo: string, n: number) => ["issue", owner, repo, n] as const,
  comments: (owner: string, repo: string, n: number) => ["comments", owner, repo, n] as const,
  intel: (owner: string, repo: string) => ["intel", owner, repo] as const,
}

export function useWhoami() {
  return useQuery({
    queryKey: keys.whoami(),
    queryFn: () => api.whoami(),
    staleTime: 5 * 60_000, // identity doesn't change mid-session
  })
}

export function useRepos() {
  return useQuery({
    queryKey: keys.repos(),
    queryFn: () => api.listRepos(),
  })
}

export function useRepo(owner: string, repo: string) {
  const qc = useQueryClient()
  return useQuery({
    queryKey: keys.repo(owner, repo),
    queryFn: () => api.getRepo(owner, repo),
    enabled: !!owner && !!repo,
    // Seed instant render from the cached repos list if we have it.
    // Background refetch still happens — this just kills the loading
    // flash when navigating from the index.
    placeholderData: () => {
      const list = qc.getQueryData<Repo[]>(keys.repos())
      return list?.find((r) => r.owner === owner && r.name === repo)
    },
  })
}

export function useTree(owner: string, repo: string, path: string) {
  return useQuery({
    queryKey: keys.tree(owner, repo, path),
    queryFn: () => api.getTree(owner, repo, path),
    enabled: !!owner && !!repo,
  })
}

export function useBlob(owner: string, repo: string, path: string) {
  return useQuery({
    queryKey: keys.blob(owner, repo, path),
    queryFn: () => api.getBlob(owner, repo, path),
    enabled: !!owner && !!repo && !!path,
  })
}

export function useIssues(owner: string, repo: string, query: string) {
  return useQuery({
    queryKey: keys.issues(owner, repo, query),
    queryFn: () => api.listIssues(owner, repo, query),
    enabled: !!owner && !!repo,
  })
}

export function useIssue(owner: string, repo: string, n: number) {
  const qc = useQueryClient()
  return useQuery({
    queryKey: keys.issue(owner, repo, n),
    queryFn: () => api.getIssue(owner, repo, n),
    enabled: !!owner && !!repo && Number.isFinite(n),
    // Seed from any cached issues list for this repo so the detail
    // page renders instantly when navigating from list/board. Looks
    // across all filter variants since each filter is a separate
    // cache entry.
    placeholderData: () => {
      const lists = qc.getQueriesData<Issue[]>({ queryKey: keys.issues(owner, repo) })
      for (const [, data] of lists) {
        const found = data?.find((i) => i.number === n)
        if (found) return found
      }
      return undefined
    },
  })
}

export function useComments(owner: string, repo: string, n: number) {
  return useQuery({
    queryKey: keys.comments(owner, repo, n),
    queryFn: () => api.listComments(owner, repo, n),
    enabled: !!owner && !!repo && Number.isFinite(n),
  })
}

export function useIntel(owner: string, repo: string) {
  return useQuery({
    queryKey: keys.intel(owner, repo),
    queryFn: () => api.getIntel(owner, repo),
    enabled: !!owner && !!repo,
  })
}
