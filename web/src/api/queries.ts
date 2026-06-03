import { useInfiniteQuery, useQuery, useQueryClient } from "@tanstack/react-query"
import { api } from "@/api/client"
import type { CIRun, CodeCommentState, Issue, Repo } from "@/api/types"

// Query keys live in one place so mutations can invalidate consistently.
// Pattern: hierarchical arrays so `["issues", owner, repo]` invalidation
// also covers `["issues", owner, repo, "filter-query"]` automatically.
export const keys = {
  whoami: () => ["whoami"] as const,
  tokens: () => ["tokens"] as const,
  sshKeys: () => ["sshKeys"] as const,
  agentSettings: () => ["agentSettings"] as const,
  repos: () => ["repos"] as const,
  allIssues: (query = "") => (query ? (["allIssues", query] as const) : (["allIssues"] as const)),
  allPulls: (state = "", query = "") => ["allPulls", state, query] as const,
  allRuns: (kind = "", query = "") => ["allRuns", kind, query] as const,
  repo: (owner: string, repo: string) => ["repo", owner, repo] as const,
  refs: (owner: string, repo: string) => ["refs", owner, repo] as const,
  tree: (owner: string, repo: string, path: string, ref = "") =>
    ["tree", owner, repo, path, ref] as const,
  blob: (owner: string, repo: string, path: string, ref = "") =>
    ["blob", owner, repo, path, ref] as const,
  commits: (owner: string, repo: string, path = "", page = 1, perPage = 0, ref = "") =>
    ["commits", owner, repo, path, page, perPage, ref] as const,
  commit: (owner: string, repo: string, sha: string) => ["commit", owner, repo, sha] as const,
  treeCommits: (owner: string, repo: string, path: string, ref = "") =>
    ["treeCommits", owner, repo, path, ref] as const,
  codeComments: (
    owner: string,
    repo: string,
    ref = "",
    path = "",
    state: CodeCommentState = "open"
  ) => ["codeComments", owner, repo, ref, path, state] as const,
  issues: (owner: string, repo: string, query = "") =>
    query ? (["issues", owner, repo, query] as const) : (["issues", owner, repo] as const),
  issue: (owner: string, repo: string, n: number) => ["issue", owner, repo, n] as const,
  issueCommits: (owner: string, repo: string, n: number) =>
    ["issueCommits", owner, repo, n] as const,
  comments: (owner: string, repo: string, n: number) => ["comments", owner, repo, n] as const,
  intel: (owner: string, repo: string) => ["intel", owner, repo] as const,
  intelOverview: (owner: string, repo: string) => ["intelOverview", owner, repo] as const,
  intelPackageGraph: (owner: string, repo: string) => ["intelPackageGraph", owner, repo] as const,
  intelFileSummary: (owner: string, repo: string, path: string) =>
    ["intelFileSummary", owner, repo, path] as const,
  intelSummaries: (owner: string, repo: string) => ["intelSummaries", owner, repo] as const,
  ciRuns: (owner: string, repo: string) => ["ciRuns", owner, repo] as const,
  ciRun: (owner: string, repo: string, n: number) => ["ciRun", owner, repo, n] as const,
  compare: (owner: string, repo: string, base: string, head: string) =>
    ["compare", owner, repo, base, head] as const,
  pulls: (owner: string, repo: string, state = "", query = "") =>
    ["pulls", owner, repo, state, query] as const,
  pull: (owner: string, repo: string, n: number) => ["pull", owner, repo, n] as const,
}

// A run is "live" until it reaches a terminal state. Lists and detail views
// poll while anything is live so status/duration tick without a manual refresh;
// once everything settles, polling stops. "finishing" is transient and
// server-driven (branch + summary handoff → terminal), so it must keep polling;
// "awaiting_input" is genuinely idle (the send-turn mutation drives it onward),
// so it stays excluded.
function isLiveStatus(status: CIRun["status"]): boolean {
  return status === "queued" || status === "running" || status === "finishing"
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

export function useSSHKeys() {
  return useQuery({
    queryKey: keys.sshKeys(),
    queryFn: () => api.listSSHKeys(),
  })
}

export function useAgentSettings() {
  return useQuery({
    queryKey: keys.agentSettings(),
    queryFn: () => api.getAgentSettings(),
  })
}

export function useRepos() {
  return useQuery({
    queryKey: keys.repos(),
    queryFn: () => api.listRepos(),
  })
}

// Cross-repo aggregate feeds for the top-level (non-repo) list views. Each row
// carries its owning repo, so the list pages can link into the per-repo detail
// routes without a separate lookup.
export function useAllIssues(query: string) {
  return useQuery({
    queryKey: keys.allIssues(query),
    queryFn: () => api.listAllIssues(query),
  })
}

export function useAllPulls(state = "", query = "") {
  return useQuery({
    queryKey: keys.allPulls(state, query),
    queryFn: () => api.listAllPulls(state, query),
  })
}

export function useAllRuns(kind: "" | "ci" | "agent" = "", query = "") {
  return useQuery({
    queryKey: keys.allRuns(kind, query),
    queryFn: () => api.listAllRuns(kind, query),
    // Poll the fleet-wide feed while any run is still live, mirroring useCIRuns,
    // so freshly triggered runs tick toward terminal without a manual refresh.
    refetchInterval: (q) => (q.state.data?.some((r) => isLiveStatus(r.status)) ? 3000 : false),
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

export function useRefs(owner: string, repo: string) {
  return useQuery({
    queryKey: keys.refs(owner, repo),
    queryFn: () => api.listRefs(owner, repo),
    enabled: !!owner && !!repo,
    staleTime: 60_000,
  })
}

export function useTree(owner: string, repo: string, path: string, ref = "") {
  return useQuery({
    queryKey: keys.tree(owner, repo, path, ref),
    queryFn: () => api.getTree(owner, repo, path, ref),
    enabled: !!owner && !!repo,
  })
}

export function useBlob(owner: string, repo: string, path: string, ref = "") {
  return useQuery({
    queryKey: keys.blob(owner, repo, path, ref),
    queryFn: () => api.getBlob(owner, repo, path, ref),
    enabled: !!owner && !!repo && !!path,
  })
}

export function useCommits(
  owner: string,
  repo: string,
  opts: { path?: string; page?: number; perPage?: number; ref?: string } = {}
) {
  const { path = "", page = 1, perPage = 0, ref = "" } = opts
  return useQuery({
    queryKey: keys.commits(owner, repo, path, page, perPage, ref),
    queryFn: () =>
      api.getCommits(owner, repo, {
        path,
        page,
        perPage: perPage || undefined,
        ref: ref || undefined,
      }),
    enabled: !!owner && !!repo,
  })
}

// Paginated commit history for the commits page. TanStack owns the page
// accumulation and per-filter cache: changing owner/repo/path swaps to a fresh
// query (no manual reset), and pages flatten out of `data.pages`.
export function useInfiniteCommits(
  owner: string,
  repo: string,
  opts: { path?: string; perPage?: number; ref?: string } = {}
) {
  const { path = "", perPage = 0, ref = "" } = opts
  return useInfiniteQuery({
    queryKey: ["commits", "infinite", owner, repo, path, perPage, ref] as const,
    queryFn: ({ pageParam }) =>
      api.getCommits(owner, repo, {
        path,
        page: pageParam,
        perPage: perPage || undefined,
        ref: ref || undefined,
      }),
    initialPageParam: 1,
    getNextPageParam: (lastPage, allPages) => (lastPage.has_more ? allPages.length + 1 : undefined),
    enabled: !!owner && !!repo,
  })
}

export function useCodeComments(
  owner: string,
  repo: string,
  opts: { ref?: string; path?: string; state?: CodeCommentState } = {}
) {
  const { ref = "", path = "", state = "open" } = opts
  return useQuery({
    queryKey: keys.codeComments(owner, repo, ref, path, state),
    queryFn: () => api.listCodeComments(owner, repo, { ref, path, state }),
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

export function useTreeCommits(owner: string, repo: string, path: string, ref = "") {
  return useQuery({
    queryKey: keys.treeCommits(owner, repo, path, ref),
    queryFn: () => api.getTreeCommits(owner, repo, path, ref),
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

// Commits whose message references this issue (#n). Resolved server-side from
// git history, so it changes only when someone pushes; a short stale window
// keeps the detail page from refetching on every navigation.
export function useIssueCommits(owner: string, repo: string, n: number) {
  return useQuery({
    queryKey: keys.issueCommits(owner, repo, n),
    queryFn: () => api.getIssueCommits(owner, repo, n),
    enabled: !!owner && !!repo && Number.isFinite(n),
    staleTime: 60_000,
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

// The internal package import DAG dex computed for the repo — backs the
// Explore "Map of the codebase" layered ranking. One cheap (no-LLM) dex call,
// gated on dex up + repo indexed and cached 5m alongside the overview.
export function useIntelPackageGraph(owner: string, repo: string, enabled: boolean) {
  return useQuery({
    queryKey: keys.intelPackageGraph(owner, repo),
    queryFn: () => api.getIntelPackageGraph(owner, repo),
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

// Poll the list while any run is still live, so a freshly pushed run shows
// progress without a refresh; stop once everything is terminal. Shared by
// useCIRuns and useCommitCIStatus, the two readers of this query.
function ciRunsRefetchInterval(q: { state: { data?: CIRun[] } }) {
  return q.state.data?.some((r) => isLiveStatus(r.status)) ? 3000 : false
}

export function useCIRuns(owner: string, repo: string, kind: "" | "ci" | "agent" = "", query = "") {
  const params = new URLSearchParams(query)
  return useQuery({
    // kind/query are appended so a mutation invalidating the keys.ciRuns prefix
    // still refreshes every variant (react-query matches by prefix), and each
    // filter combination caches independently.
    queryKey: [...keys.ciRuns(owner, repo), kind, query],
    queryFn: () =>
      api.listCIRuns(owner, repo, {
        kind,
        state: params.get("state") ?? undefined,
        q: params.get("q") ?? undefined,
      }),
    enabled: !!owner && !!repo,
    refetchInterval: ciRunsRefetchInterval,
  })
}

// useCommitCIStatus maps each commit SHA to its most recent CI run, for the
// status badges shown beside commits (history list, last-commit bars). It
// shares the ciRuns cache key with useCIRuns — one fetch, two readers — and
// gates on `enabled` so repos without CI never fetch. The list is newest-first,
// so the first run seen for a SHA is its latest.
export function useCommitCIStatus(owner: string, repo: string, enabled = true) {
  return useQuery({
    queryKey: keys.ciRuns(owner, repo),
    queryFn: () => api.listCIRuns(owner, repo),
    enabled: !!owner && !!repo && enabled,
    refetchInterval: ciRunsRefetchInterval,
    select: (runs) => {
      const byCommit = new Map<string, CIRun>()
      for (const run of runs) if (!byCommit.has(run.commit_sha)) byCommit.set(run.commit_sha, run)
      return byCommit
    },
  })
}

// useCompare fetches the three-dot diff of head vs base. Enabled only when both
// branches are chosen; a self-compare (base === head) is skipped since the
// server rejects it and there's nothing to show.
export function useCompare(owner: string, repo: string, base: string, head: string) {
  return useQuery({
    queryKey: keys.compare(owner, repo, base, head),
    queryFn: () => api.getCompare(owner, repo, base, head),
    enabled: !!owner && !!repo && !!base && !!head && base !== head,
  })
}

export function usePulls(owner: string, repo: string, state = "", query = "") {
  return useQuery({
    queryKey: keys.pulls(owner, repo, state, query),
    queryFn: () => api.listPulls(owner, repo, state, query),
    enabled: !!owner && !!repo,
  })
}

export function usePull(owner: string, repo: string, n: number) {
  return useQuery({
    queryKey: keys.pull(owner, repo, n),
    queryFn: () => api.getPull(owner, repo, n),
    enabled: !!owner && !!repo && Number.isFinite(n),
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
