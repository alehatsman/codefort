import type {
  AgentSettings,
  AgentTurn,
  Blob,
  UpdateAgentSettingsInput,
  CIRun,
  CIRunDetail,
  CIRunExecutionModel,
  CIRunToolProfile,
  ClaimIssueInput,
  CodeComment,
  CodeCommentState,
  Comment,
  Commit,
  Compare,
  CommitDetail,
  CommitList,
  TreeCommits,
  CreateCodeCommentInput,
  CreateCommentInput,
  CreatedToken,
  CreateIssueInput,
  CreatePullRequestInput,
  CreateRepoInput,
  CreateSSHKeyInput,
  CreateTokenInput,
  MergeRequestInput,
  MergeResult,
  PullRequest,
  PullRequestDetail,
  RefList,
  UpdateCodeCommentInput,
  UpdatePullRequestInput,
  Intel,
  IntelFileSummary,
  IntelOverview,
  IntelPackageGraph,
  IntelSummaries,
  IntelSearchInput,
  IntelSearchResult,
  Issue,
  IssueWithRepo,
  PullRequestWithRepo,
  CIRunWithRepo,
  Repo,
  SpecContent,
  SpecList,
  SpecSearchResult,
  SSHKey,
  WriteSpecInput,
  WriteSpecResult,
  Token,
  Tree,
  UpdateIssueInput,
  UpdateRepoInput,
  Whoami,
} from "@/api/types"

const TOKEN_KEY = "moongit_token"

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}

export function setToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token)
}

/**
 * verifyToken checks a candidate token against the server without persisting
 * it, so the gate can reject an invalid token before it ever lands in
 * localStorage. Returns true only on a 2xx /api/whoami; a 401 (or any other
 * non-ok / network failure) returns false.
 */
export async function verifyToken(token: string): Promise<boolean> {
  try {
    const resp = await fetch("/api/whoami", {
      headers: { Accept: "application/json", Authorization: `Bearer ${token}` },
    })
    return resp.ok
  } catch {
    return false
  }
}

export function clearToken(): void {
  localStorage.removeItem(TOKEN_KEY)
}

/**
 * ApiError carries the HTTP status so callers can branch on 401, 409, etc.
 * `body` holds the parsed JSON error body when there is one, so callers can
 * read structured fields (e.g. a merge conflict's `conflicts` paths) beyond the
 * flat message.
 */
export class ApiError extends Error {
  status: number
  body?: unknown
  constructor(status: number, message: string, body?: unknown) {
    super(message)
    this.status = status
    this.body = body
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
    let body: unknown
    try {
      body = JSON.parse(raw)
      if (body && typeof (body as { error?: unknown }).error === "string") {
        message = (body as { error: string }).error
      }
    } catch {
      // raw stays as message; body stays undefined
    }
    throw new ApiError(resp.status, message, body)
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

  listSSHKeys: () => request<SSHKey[]>("/api/ssh-keys"),
  createSSHKey: (body: CreateSSHKeyInput) =>
    request<SSHKey>("/api/ssh-keys", { method: "POST", body }),
  deleteSSHKey: (id: number) => request<void>(`/api/ssh-keys/${id}`, { method: "DELETE" }),

  // Cross-repo aggregate feeds backing the top-level (non-repo) list views.
  // Each row carries its owning repo (`repo`) so it can link to the per-repo
  // detail route. Query params mirror the per-repo list endpoints.
  listAllIssues: (query = "") => request<IssueWithRepo[]>(`/api/issues${query ? `?${query}` : ""}`),
  listAllPulls: (state = "", query = "") => {
    const q = new URLSearchParams()
    if (state) q.set("state", state)
    if (query) q.set("q", query)
    const qs = q.toString()
    return request<PullRequestWithRepo[]>(`/api/pulls${qs ? `?${qs}` : ""}`)
  },
  // query is an extra param string (state, q, limit) the caller pre-builds,
  // mirroring listAllIssues; kind is merged in so the Pipelines/Agents tabs stay
  // scoped. Filtering is applied server-side so the row cap covers the match set.
  listAllRuns: (kind = "", query = "") => {
    const p = new URLSearchParams(query)
    if (kind) p.set("kind", kind)
    const qs = p.toString()
    return request<CIRunWithRepo[]>(`/api/runs${qs ? `?${qs}` : ""}`)
  },

  listRepos: () => request<Repo[]>("/api/repos"),
  createRepo: (body: CreateRepoInput) => request<Repo>("/api/repos", { method: "POST", body }),
  getRepo: (owner: string, repo: string) => request<Repo>(`/api/repos/${owner}/${repo}`),
  updateRepo: (owner: string, repo: string, body: UpdateRepoInput) =>
    request<Repo>(`/api/repos/${owner}/${repo}`, { method: "PATCH", body }),
  deleteRepo: (owner: string, repo: string) =>
    request<void>(`/api/repos/${owner}/${repo}`, { method: "DELETE" }),

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
  getIssueCommits: (owner: string, repo: string, n: number) =>
    request<Commit[]>(`/api/repos/${owner}/${repo}/issues/${n}/commits`),
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
  getIntelPackageGraph: (owner: string, repo: string) =>
    request<IntelPackageGraph>(`/api/repos/${owner}/${repo}/intel/package-graph`),
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

  listSpecs: (owner: string, repo: string, ref = "") => {
    const q = new URLSearchParams()
    if (ref) q.set("ref", ref)
    const qs = q.toString()
    return request<SpecList>(`/api/repos/${owner}/${repo}/specs${qs ? `?${qs}` : ""}`)
  },
  // path is repo-relative ("specs/ssh-transport.md"); the spec route's {path...}
  // is taken relative to specs/, so strip the leading "specs/" before appending.
  getSpec: (owner: string, repo: string, path: string, ref = "") => {
    const rel = path.replace(/^specs\//, "")
    const segs = rel.split("/").map(encodeURIComponent).join("/")
    const q = new URLSearchParams()
    if (ref) q.set("ref", ref)
    const qs = q.toString()
    return request<SpecContent>(`/api/repos/${owner}/${repo}/specs/${segs}${qs ? `?${qs}` : ""}`)
  },
  searchSpecs: (owner: string, repo: string, query: string) =>
    request<SpecSearchResult>(`/api/repos/${owner}/${repo}/specs/search`, {
      method: "POST",
      body: { query },
    }),
  writeSpec: (owner: string, repo: string, path: string, body: WriteSpecInput) => {
    const rel = path.replace(/^specs\//, "")
    const segs = rel.split("/").map(encodeURIComponent).join("/")
    return request<WriteSpecResult>(`/api/repos/${owner}/${repo}/specs/${segs}`, {
      method: "PUT",
      body,
    })
  },

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

  getCompare: (owner: string, repo: string, base: string, head: string) => {
    const q = new URLSearchParams({ base, head })
    return request<Compare>(`/api/repos/${owner}/${repo}/compare?${q.toString()}`)
  },

  listPulls: (owner: string, repo: string, state = "", query = "") => {
    const q = new URLSearchParams()
    if (state) q.set("state", state)
    if (query) q.set("q", query)
    const qs = q.toString()
    return request<PullRequest[]>(`/api/repos/${owner}/${repo}/pulls${qs ? `?${qs}` : ""}`)
  },
  getPull: (owner: string, repo: string, n: number) =>
    request<PullRequestDetail>(`/api/repos/${owner}/${repo}/pulls/${n}`),
  createPull: (owner: string, repo: string, body: CreatePullRequestInput) =>
    request<PullRequest>(`/api/repos/${owner}/${repo}/pulls`, { method: "POST", body }),
  updatePull: (owner: string, repo: string, n: number, body: UpdatePullRequestInput) =>
    request<PullRequest>(`/api/repos/${owner}/${repo}/pulls/${n}`, { method: "PATCH", body }),
  mergePull: (owner: string, repo: string, n: number, body: MergeRequestInput) =>
    request<MergeResult>(`/api/repos/${owner}/${repo}/pulls/${n}/merge`, { method: "POST", body }),

  listCIRuns: (
    owner: string,
    repo: string,
    opts: { limit?: number; kind?: string; state?: string; q?: string } = {}
  ) => {
    const p = new URLSearchParams()
    if (opts.limit) p.set("limit", String(opts.limit))
    if (opts.kind) p.set("kind", opts.kind)
    if (opts.state) p.set("state", opts.state)
    if (opts.q) p.set("q", opts.q)
    const qs = p.toString()
    return request<CIRun[]>(`/api/repos/${owner}/${repo}/runs${qs ? `?${qs}` : ""}`)
  },
  triggerCIRun: (owner: string, repo: string, ref: string) =>
    request<CIRun>(`/api/repos/${owner}/${repo}/runs`, { method: "POST", body: { ref } }),
  getCIRun: (owner: string, repo: string, n: number) =>
    request<CIRunDetail>(`/api/repos/${owner}/${repo}/runs/${n}`),
  rerunCIRun: (owner: string, repo: string, n: number) =>
    request<CIRun>(`/api/repos/${owner}/${repo}/runs/${n}/rerun`, { method: "POST" }),

  // spawnAgent starts an agent run for an issue. ref pins the base the agent
  // checks out (optional; the server defaults to the repo's HEAD); model picks
  // the execution model (optional; the server defaults from settings). The
  // resulting agent run shares the ci_runs surface, so it shows up under
  // Pipelines and streams over the same run/job event endpoints.
  spawnAgent: (
    owner: string,
    repo: string,
    n: number,
    opts?: {
      ref?: string
      model?: CIRunExecutionModel
      allowShell?: boolean
      toolProfile?: CIRunToolProfile
    }
  ) =>
    request<CIRun>(`/api/repos/${owner}/${repo}/issues/${n}/agent`, {
      method: "POST",
      body: {
        ...(opts?.ref ? { ref: opts.ref } : {}),
        ...(opts?.model ? { model: opts.model } : {}),
        ...(opts?.allowShell ? { allow_shell: true } : {}),
        ...(opts?.toolProfile ? { tool_profile: opts.toolProfile } : {}),
      },
    }),
  // createAgentTurn queues a follow-up message on an agent run; the dispatch
  // loop resumes the session and streams the response onto the run's events.
  createAgentTurn: (owner: string, repo: string, n: number, text: string) =>
    request<AgentTurn>(`/api/repos/${owner}/${repo}/runs/${n}/turns`, {
      method: "POST",
      body: { text },
    }),
  // finishAgentRun accepts a parked agent run; the server hands off (branch +
  // summary comment) and finalizes it.
  finishAgentRun: (owner: string, repo: string, n: number) =>
    request<CIRun>(`/api/repos/${owner}/${repo}/runs/${n}/finish`, { method: "POST" }),
  // cancelAgentRun force-stops an agent run from any non-terminal state
  // (unlike finish, which needs a parked run): it interrupts an in-flight turn
  // and discards the workspace — no branch is handed off.
  cancelAgentRun: (owner: string, repo: string, n: number) =>
    request<CIRun>(`/api/repos/${owner}/${repo}/runs/${n}/cancel`, { method: "POST" }),

  // Global agent settings (write-only Claude token).
  getAgentSettings: () => request<AgentSettings>("/api/settings/agent"),
  updateAgentSettings: (body: UpdateAgentSettingsInput) =>
    request<AgentSettings>("/api/settings/agent", { method: "PUT", body }),
}
