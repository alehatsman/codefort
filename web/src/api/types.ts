// Hand-mirrored from internal/api/types.go. Keep in sync.

export type IssueState = "todo" | "in_progress" | "done" | "closed"

export const ISSUE_STATES: readonly IssueState[] = [
  "todo",
  "in_progress",
  "done",
  "closed",
] as const

export interface Repo {
  id: number
  owner: string
  name: string
  created_at: string
  open_issues: number
  total_issues: number
}

export interface Issue {
  id: number
  number: number
  title: string
  body?: string
  author: string
  state: IssueState
  assignee: string | null
  created_at: string
  updated_at: string
}

export interface Comment {
  id: number
  issue_id: number
  author: string
  body: string
  created_at: string
}

export interface CreateRepoInput {
  owner: string
  name: string
}

export interface CreateIssueInput {
  title: string
  body?: string
}

export interface CreateCommentInput {
  body: string
}

export interface ClaimIssueInput {
  state?: IssueState
}

export interface UpdateIssueInput {
  state: IssueState
}

export interface Whoami {
  name: string
}

// --- Code browser ---

export interface TreeEntry {
  name: string
  path: string
  type: "blob" | "tree"
  size?: number
}

export interface Tree {
  ref: string
  path: string
  entries: TreeEntry[]
}

export interface Blob {
  ref: string
  path: string
  size: number
  binary: boolean
  too_large: boolean
  content: string
}

// --- Intel (dex integration) ---

export interface IntelService {
  endpoint: string
  reachable: boolean
  model: string
  version: string
}

export interface IntelProject {
  root: string
  chunks: number
  files: number
  dim: number
  embed_model: string
  last_indexed: string
  pending_summaries: number
}

export interface Intel {
  enabled: boolean
  found: boolean
  service?: IntelService
  project?: IntelProject
}

export type IntelSearchKind = "semantic" | "symbol" | "ask" | "callers" | "callees"

export interface IntelSearchInput {
  query: string
  kind: IntelSearchKind
}

export interface IntelHit {
  path: string
  kind: string
  start_line: number
  end_line: number
  score: number
  role?: string
  content?: string
}

export interface IntelSearchResult {
  status: string
  hint?: string
  hits: IntelHit[]
}
