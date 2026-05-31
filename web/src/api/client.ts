import type {
  Blob,
  CIRun,
  CIRunDetail,
  ClaimIssueInput,
  CodeComment,
  CodeCommentState,
  Comment,
  CommitDetail,
  CommitList,
  TreeCommits,
  CreateCodeCommentInput,
  CreateCommentInput,
  CreatedToken,
  CreateIssueInput,
  CreateRepoInput,
  CreateTokenInput,
  RefList,
  UpdateCodeCommentInput,
  Intel,
  IntelFileSummary,
  IntelOverview,
  IntelSummaries,
  IntelSearchInput,
  IntelSearchResult,
  Issue,
  Repo,
  Token,
  Tree,
  UpdateIssueInput,
  UpdateRepoInput,
  Whoami,
} from "./types"

const TOKEN_KEY = "moongit_token"

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}

export function setToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token)
}

export function clearToken(): void {
  localStorage.removeItem(TOKEN_KEY)
}

/** ApiError carries the HTTP status so callers can branch on 401, 409, etc. */
export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

interface RequestOpts {
  method?: string
  body?: unknown
}

async function request<T>(path: string, opts: RequestOpts = {}): Promise<T> {
  const token = getToken()
  const headers = new Headers()
  headers.set("Accept", "application/json")
  if (opts.body !== undefined) {
    headers.set("Content-Type", "application/json")
  }
  if (token) {
    headers.set("Authorization", `Bearer ${token}`)
  }

  const resp = await fetch(path, {
    method: opts.method ?? "GET",
    headers,
    body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
  })

  const raw = await resp.text()
  if (!resp.ok) {
    let message = raw || resp.statusText
    try {
      const parsed = JSON.parse(raw)
      if (parsed && typeof parsed.error === "string") message = parsed.error
    } catch {
      // raw stays as message
    }
    throw new ApiError(resp.status, message)
  }
  if (!raw) return undefined as T
  return JSON.parse(raw) as T
}

export const api = {
  whoami: () => request<Whoami>("/api/whoami"),

  listTokens: () => request<Token[]>("/api/tokens"),
  createToken: (body: CreateTokenInput) =>
    request<CreatedToken>("/api/tokens", { method: "POST", body }),
  revokeToken: (id: number) => request<void>(`/api/tokens/${id}`, { method: "DELETE" }),

  listRepos: () => request<Repo[]>("/api/repos"),
  createRepo: (body: CreateRepoInput) => request<Repo>("/api/repos", { method: "POST", body }),
  getRepo: (owner: string, repo: string) => request<Repo>(`/api/repos/${owner}/${repo}`),
  updateRepo: (owner: string, repo: string, body: UpdateRepoInput) =>
    request<Repo>(`/api/repos/${owner}/${repo}`, { method: "PATCH", body }),

  listRefs: (owner: string, repo: string) => request<RefList>(`/api/repos/${owner}/${repo}/refs`),

  getTree: (owner: string, repo: string, path = "", ref = "") => {
    const q = new URLSearchParams()
    if (path) q.set("path", path)
    if (ref) q.set("ref", ref)
    const qs = q.toString()
    return request<Tree>(`/api/repos/${owner}/${repo}/tree${qs ? `?${qs}` : ""}`)
  },
  getBlob: (owner: string, repo: string, path: string, ref = "") => {
    const q = new URLSearchParams({ path })
    if (ref) q.set("ref", ref)
    return request<Blob>(`/api/repos/${owner}/${repo}/blob?${q.toString()}`)
  },
  // Raw bytes (e.g. images embedded in a rendered README). Fetched with the
  // Bearer header, so it can't be a plain <img src>; the caller turns the
  // returned Blob into an object URL.
  getRawBlob: async (owner: string, repo: string, path: string): Promise<globalThis.Blob> => {
    const token = getToken()
    const headers = new Headers()
    if (token) headers.set("Authorization", `Bearer ${token}`)
    const resp = await fetch(`/api/repos/${owner}/${repo}/raw?path=${encodeURIComponent(path)}`, {
      headers,
    })
    if (!resp.ok) throw new ApiError(resp.status, resp.statusText)
    return resp.blob()
  },

  getCommits: (
    owner: string,
    repo: string,
    opts: { path?: string; page?: number; perPage?: number; ref?: string } = {}
  ) => {
    const q = new URLSearchParams()
    if (opts.path) q.set("path", opts.path)
    if (opts.page) q.set("page", String(opts.page))
    if (opts.perPage) q.set("per_page", String(opts.perPage))
    if (opts.ref) q.set("ref", opts.ref)
    const qs = q.toString()
    return request<CommitList>(`/api/repos/${owner}/${repo}/commits${qs ? `?${qs}` : ""}`)
  },
  getCommit: (owner: string, repo: string, sha: string) =>
    request<CommitDetail>(`/api/repos/${owner}/${repo}/commit/${sha}`),
  getTreeCommits: (owner: string, repo: string, path = "", ref = "") => {
    const q = new URLSearchParams()
    if (path) q.set("path", path)
    if (ref) q.set("ref", ref)
    const qs = q.toString()
    return request<TreeCommits>(`/api/repos/${owner}/${repo}/tree-commits${qs ? `?${qs}` : ""}`)
  },

  listIssues: (owner: string, repo: string, query = "") =>
    request<Issue[]>(`/api/repos/${owner}/${repo}/issues${query ? `?${query}` : ""}`),
  getIssue: (owner: string, repo: string, n: number) =>
    request<Issue>(`/api/repos/${owner}/${repo}/issues/${n}`),
  createIssue: (owner: string, repo: string, body: CreateIssueInput) =>
    request<Issue>(`/api/repos/${owner}/${repo}/issues`, { method: "POST", body }),
  updateIssue: (owner: string, repo: string, n: number, body: UpdateIssueInput) =>
    request<Issue>(`/api/repos/${owner}/${repo}/issues/${n}`, { method: "PATCH", body }),
  deleteIssue: (owner: string, repo: string, n: number) =>
    request<void>(`/api/repos/${owner}/${repo}/issues/${n}`, { method: "DELETE" }),
  claimIssue: (owner: string, repo: string, n: number, body: ClaimIssueInput) =>
    request<Issue>(`/api/repos/${owner}/${repo}/issues/${n}/claim`, { method: "POST", body }),
  unclaimIssue: (owner: string, repo: string, n: number) =>
    request<Issue>(`/api/repos/${owner}/${repo}/issues/${n}/unclaim`, { method: "POST" }),

  listComments: (owner: string, repo: string, n: number) =>
    request<Comment[]>(`/api/repos/${owner}/${repo}/issues/${n}/comments`),
  createComment: (owner: string, repo: string, n: number, body: CreateCommentInput) =>
    request<Comment>(`/api/repos/${owner}/${repo}/issues/${n}/comments`, {
      method: "POST",
      body,
    }),
  deleteComment: (owner: string, repo: string, n: number, commentID: number) =>
    request<void>(`/api/repos/${owner}/${repo}/issues/${n}/comments/${commentID}`, {
      method: "DELETE",
    }),

  getIntel: (owner: string, repo: string) => request<Intel>(`/api/repos/${owner}/${repo}/intel`),
  getIntelOverview: (owner: string, repo: string) =>
    request<IntelOverview>(`/api/repos/${owner}/${repo}/intel/overview`),
  getIntelFileSummary: (owner: string, repo: string, path: string) =>
    request<IntelFileSummary>(
      `/api/repos/${owner}/${repo}/intel/file-summary?path=${encodeURIComponent(path)}`
    ),
  getIntelSummaries: (owner: string, repo: string) =>
    request<IntelSummaries>(`/api/repos/${owner}/${repo}/intel/summaries`),
  intelSearch: (owner: string, repo: string, body: IntelSearchInput) =>
    request<IntelSearchResult>(`/api/repos/${owner}/${repo}/intel/search`, {
      method: "POST",
      body,
    }),

  listCodeComments: (
    owner: string,
    repo: string,
    opts: { ref?: string; path?: string; state?: CodeCommentState } = {}
  ) => {
    const q = new URLSearchParams()
    if (opts.ref) q.set("ref", opts.ref)
    if (opts.path) q.set("path", opts.path)
    if (opts.state) q.set("state", opts.state)
    const qs = q.toString()
    return request<CodeComment[]>(`/api/repos/${owner}/${repo}/code-comments${qs ? `?${qs}` : ""}`)
  },
  createCodeComment: (owner: string, repo: string, body: CreateCodeCommentInput) =>
    request<CodeComment>(`/api/repos/${owner}/${repo}/code-comments`, { method: "POST", body }),
  updateCodeComment: (owner: string, repo: string, id: number, body: UpdateCodeCommentInput) =>
    request<CodeComment>(`/api/repos/${owner}/${repo}/code-comments/${id}`, {
      method: "PATCH",
      body,
    }),
  deleteCodeComment: (owner: string, repo: string, id: number) =>
    request<void>(`/api/repos/${owner}/${repo}/code-comments/${id}`, { method: "DELETE" }),

  listCIRuns: (owner: string, repo: string, limit = 0) =>
    request<CIRun[]>(`/api/repos/${owner}/${repo}/ci/runs${limit ? `?limit=${limit}` : ""}`),
  triggerCIRun: (owner: string, repo: string, ref: string) =>
    request<CIRun>(`/api/repos/${owner}/${repo}/ci/runs`, { method: "POST", body: { ref } }),
  getCIRun: (owner: string, repo: string, n: number) =>
    request<CIRunDetail>(`/api/repos/${owner}/${repo}/ci/runs/${n}`),
  rerunCIRun: (owner: string, repo: string, n: number) =>
    request<CIRun>(`/api/repos/${owner}/${repo}/ci/runs/${n}/rerun`, { method: "POST" }),
}
