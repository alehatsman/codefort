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
  ci_enabled: boolean
}

export interface UpdateRepoInput {
  ci_enabled?: boolean
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

// --- API tokens ---

export interface Token {
  id: number
  name: string
  created_at: string
  last_used_at?: string
  revoked_at?: string
}

export interface CreateTokenInput {
  name: string
}

// CreatedToken is returned once, by POST /api/tokens — the only time the
// plaintext `secret` is ever sent. Surface it immediately; it's gone after.
export interface CreatedToken extends Token {
  secret: string
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

export interface Commit {
  sha: string
  short_sha: string
  subject: string
  body?: string
  author: string
  email: string
  date: string
}

export interface CommitList {
  ref: string
  path?: string
  commits: Commit[]
  has_more: boolean
}

export type DiffLineKind = "context" | "add" | "del"

export interface DiffLine {
  kind: DiffLineKind
  // 1-based line numbers on each side; 0 where the line is absent (an add has
  // no old number, a del no new number).
  old: number
  new: number
  text: string
}

export interface DiffHunk {
  header: string
  lines: DiffLine[]
}

export type DiffStatus = "added" | "modified" | "deleted" | "renamed"

export interface DiffFile {
  old_path: string
  new_path: string
  status: DiffStatus
  binary: boolean
  additions: number
  deletions: number
  hunks: DiffHunk[]
}

export interface CommitDetail {
  commit: Commit
  parents: string[]
  files: DiffFile[]
  additions: number
  deletions: number
  truncated: boolean
}

export interface TreeCommits {
  ref: string
  path?: string
  total: number
  latest: Commit | null
  // child full path -> last commit touching it
  entries: Record<string, Commit>
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

export interface IntelSuggestedRead {
  path: string
  start_line: number
  end_line: number
  reason?: string
  content?: string
  truncated?: boolean
}

export interface IntelAnnotation {
  nearest_doc?: string
  tests?: string[]
  package?: string
}

export interface IntelGraphNode {
  id: string
  qualified_name?: string
  kind?: string
}

export interface IntelGraphEdge {
  from: string
  to: string
  kind?: string
}

export interface IntelGraph {
  nodes: IntelGraphNode[]
  edges: IntelGraphEdge[]
}

export interface IntelPackageSummary {
  path: string
  summary: string
}

export interface IntelOverview {
  repo_summary?: string
  packages: IntelPackageSummary[]
}

export interface IntelFileSummary {
  path: string
  summary: string
}

// Per-segment breadcrumb summaries: keyed by sub-path ("" = repo root,
// directory paths, the leaf file path), carrying only the segments dex has
// prose for. Backs the Code tab's per-crumb hover tooltips.
export interface IntelPathSummaries {
  summaries: Record<string, string>
}

// --- CI (moongitci) ---

// Mirrors storage.RunStatus. queued -> running -> a terminal state.
export type CIRunStatus = "queued" | "running" | "success" | "failed" | "canceled" | "error"

// Mirrors storage.JobStatus.
export type CIJobStatus = "queued" | "running" | "success" | "failed" | "skipped" | "error"

export interface CIRun {
  number: number
  commit_sha: string
  commit_msg?: string
  commit_author?: string
  ref: string
  event: string
  trigger?: string
  status: CIRunStatus
  created_at: string
  started_at: string | null
  finished_at: string | null
}

export interface CIJob {
  name: string
  needs?: string[]
  status: CIJobStatus
  exit_code: number | null
  started_at: string | null
  finished_at: string | null
}

export interface CIRunDetail extends CIRun {
  jobs: CIJob[]
}

// CIEvent mirrors internal/ci.Event — one entry in a job's append-only event
// stream. Seq is monotonic within a job; data shape varies by type (see the
// CI_EVENT_* constants).
export interface CIEvent {
  seq: number
  type: string
  time: number
  data?: Record<string, unknown>
}

export interface IntelSearchResult {
  status: string
  hint?: string
  hits: IntelHit[]
  /** Only populated when kind="ask". The CLI prints these and they're more
   *  actionable than the raw semantic_hits. */
  next_action?: string
  avoid?: string
  suggested_reads?: IntelSuggestedRead[]
  annotations?: Record<string, IntelAnnotation>
  graph?: IntelGraph
}
