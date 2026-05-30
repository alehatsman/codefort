import { useQuery, useQueryClient } from "@tanstack/react-query"
import { api } from "./client"
import type { CIRun, Issue, Repo } from "./types"

// Query keys live in one place so mutations can invalidate consistently.
// Pattern: hierarchical arrays so `["issues", owner, repo]` invalidation
// also covers `["issues", owner, repo, "filter-query"]` automatically.
export const keys = {
  whoami: () => ["whoami"] as const,
  tokens: () => ["tokens"] as const,
  repos: () => ["repos"] as const,
  repo: (owner: string, repo: string) => ["repo", owner, repo] as const,
  tree: (owner: string, repo: string, path: string) => ["tree", owner, repo, path] as const,
  blob: (owner: string, repo: string, path: string) => ["blob", owner, repo, path] as const,
  commits: (owner: string, repo: string, path = "", page = 1, perPage = 0) =>
    ["commits", owner, repo, path, page, perPage] as const,
  commit: (owner: string, repo: string, sha: string) => ["commit", owner, repo, sha] as const,
  treeCommits: (owner: string, repo: string, path: string) =>
    ["treeCommits", owner, repo, path] as const,
  issues: (owner: string, repo: string, query = "") =>
    query ? (["issues", owner, repo, query] as const) : (["issues", owner, repo] as const),
  issue: (owner: string, repo: string, n: number) => ["issue", owner, repo, n] as const,
  comments: (owner: string, repo: string, n: number) => ["comments", owner, repo, n] as const,
  intel: (owner: string, repo: string) => ["intel", owner, repo] as const,
  intelOverview: (owner: string, repo: string) => ["intelOverview", owner, repo] as const,
  intelFileSummary: (owner: string, repo: string, path: string) =>
    ["intelFileSummary", owner, repo, path] as const,
  intelSummaries: (owner: string, repo: string) => ["intelSummaries", owner, repo] as const,
  ciRuns: (owner: string, repo: string) => ["ciRuns", owner, repo] as const,
  ciRun: (owner: string, repo: string, n: number) => ["ciRun", owner, repo, n] as const,
}

// A run is "live" (queued or running) until it reaches a terminal state. Lists
// and detail views poll while anything is live so status/duration tick without
// a manual refresh; once everything settles, polling stops.
function isLiveStatus(status: CIRun["status"]): boolean {
  return status === "queued" || status === "running"
}

export function useWhoami() {
  return useQuery({
    queryKey: keys.whoami(),
    queryFn: () => api.whoami(),
    staleTime: 5 * 60_000, // identity doesn't change mid-session
  })
}

export function useTokens() {
  return useQuery({
    queryKey: keys.tokens(),
    queryFn: () => api.listTokens(),
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

export function useCommits(
  owner: string,
  repo: string,
  opts: { path?: string; page?: number; perPage?: number } = {}
) {
  const { path = "", page = 1, perPage = 0 } = opts
  return useQuery({
    queryKey: keys.commits(owner, repo, path, page, perPage),
    queryFn: () => api.getCommits(owner, repo, { path, page, perPage: perPage || undefined }),
    enabled: !!owner && !!repo,
  })
}

export function useCommit(owner: string, repo: string, sha: string) {
  return useQuery({
    queryKey: keys.commit(owner, repo, sha),
    queryFn: () => api.getCommit(owner, repo, sha),
    enabled: !!owner && !!repo && !!sha,
    // A commit's content is immutable, so never refetch once loaded.
    staleTime: Infinity,
  })
}

export function useTreeCommits(owner: string, repo: string, path: string) {
  return useQuery({
    queryKey: keys.treeCommits(owner, repo, path),
    queryFn: () => api.getTreeCommits(owner, repo, path),
    enabled: !!owner && !!repo,
    // Commit annotations change less often than the tree itself; a short
    // stale window avoids refetching per-file history on every navigation.
    staleTime: 60_000,
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

export function useIntelOverview(owner: string, repo: string, enabled: boolean) {
  return useQuery({
    queryKey: keys.intelOverview(owner, repo),
    queryFn: () => api.getIntelOverview(owner, repo),
    // Two dex round trips per fetch — only run when we know dex is up
    // and this repo is indexed (gated on useIntel's found flag).
    enabled: enabled && !!owner && !!repo,
    staleTime: 5 * 60_000,
  })
}

export function useIntelFileSummary(owner: string, repo: string, path: string, enabled: boolean) {
  return useQuery({
    queryKey: keys.intelFileSummary(owner, repo, path),
    queryFn: () => api.getIntelFileSummary(owner, repo, path),
    // One dex round trip per file view — gate on dex up + repo indexed.
    enabled: enabled && !!owner && !!repo && !!path,
    staleTime: 5 * 60_000,
  })
}

// Every dex summary for the repo as one path→prose map — powers both the
// breadcrumb and the file tree. One cached query per repo (staleTime 5m), so
// navigating between folders/files reuses it with no extra round trips.
export function useIntelSummaries(owner: string, repo: string, enabled: boolean) {
  return useQuery({
    queryKey: keys.intelSummaries(owner, repo),
    queryFn: () => api.getIntelSummaries(owner, repo),
    enabled: enabled && !!owner && !!repo,
    staleTime: 5 * 60_000,
  })
}

export function useCIRuns(owner: string, repo: string) {
  return useQuery({
    queryKey: keys.ciRuns(owner, repo),
    queryFn: () => api.listCIRuns(owner, repo),
    enabled: !!owner && !!repo,
    // Poll the list while any run is still live, so a freshly pushed run shows
    // progress without a refresh; stop once everything is terminal.
    refetchInterval: (q) => (q.state.data?.some((r) => isLiveStatus(r.status)) ? 3000 : false),
  })
}

export function useCIRun(owner: string, repo: string, n: number) {
  return useQuery({
    queryKey: keys.ciRun(owner, repo, n),
    queryFn: () => api.getCIRun(owner, repo, n),
    enabled: !!owner && !!repo && Number.isFinite(n),
    // Poll run + job statuses while the run is live (the SSE stream carries the
    // log lines; this keeps the run/job status badges current).
    refetchInterval: (q) => (q.state.data && isLiveStatus(q.state.data.status) ? 2000 : false),
  })
}
