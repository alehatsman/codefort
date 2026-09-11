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
  // Status + per-repo number of the repo's most recent CI run, absent when the
  // repo has no runs. Drives the at-a-glance CI icon on the repos list.
  ci_status?: CIRunStatus
  ci_number?: number
  // At-a-glance counts for the repos-list metric grid: open PRs, unresolved
  // code-review comments, and agent runs in a non-terminal state. Always present.
  open_pulls: number
  open_reviews: number
  active_agents: number
  visibility: "public" | "private"
}

export interface UpdateRepoInput {
  ci_enabled?: boolean
  visibility?: "public" | "private"
}

export interface User {
  id: number
  name: string
  created_at: string
}

export interface RegisterInput {
  username: string
  password: string
}

export interface LoginInput {
  username: string
  password: string
}

export interface AuthResponse {
  token: Token
  secret: string
}

export interface RepoMember {
  username: string
  role: "read" | "write"
  joined_at: string
}

export interface AddMemberInput {
  username: string
  role?: "read" | "write"
}

export interface ChildIssueSummary {
  number: number
  title: string
  state: IssueState
}

export interface IssueRef {
  number: number
  title: string
  state: IssueState
}

export interface EpicProgress {
  total: number
  done: number
}

export interface Issue {
  id: number
  number: number
  title: string
  body?: string
  author: string
  state: IssueState
  assignee: string | null
  parent_number?: number
  children?: ChildIssueSummary[]
  depends_on?: IssueRef[]
  blocks?: IssueRef[]
  progress?: EpicProgress
  labels: string[]
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

// RepoRef tags a globally-listed row (issue / PR / run) with its owning repo so
// the cross-repo views can link each entry back to the per-repo detail route.
// Mirrors api.RepoRef.
export interface RepoRef {
  owner: string
  name: string
}

// The cross-repo aggregate shapes returned by GET /api/issues, /api/pulls,
// /api/runs — the per-repo row plus its owning repo. Mirror api.*WithRepo.
export interface IssueWithRepo extends Issue {
  repo: RepoRef
}

export interface PullRequestWithRepo extends PullRequest {
  repo: RepoRef
}

export interface CIRunWithRepo extends CIRun {
  repo: RepoRef
}

export interface CreateRepoInput {
  owner: string
  name: string
  visibility?: "public" | "private"
}

export interface CreateIssueInput {
  title: string
  body?: string | undefined
  labels?: string[] | undefined
}

export interface CreateCommentInput {
  body: string
}

export interface ClaimIssueInput {
  state?: IssueState
}

// Partial update: send only the fields that change. The backend treats each as
// optional (state-only requests stay wire-compatible with older clients).
export interface UpdateIssueInput {
  state?: IssueState
  title?: string
  body?: string
  labels?: string[] // non-nil = replace entire set; omit = no change
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

// --- SSH keys ---

// SSHKey is a registered public key for the git SSH transport. It belongs to a
// token (token_name is the push/pull identity). The public half isn't a
// secret, so it's returned in full on every read.
export interface SSHKey {
  id: number
  token_name: string
  fingerprint: string
  comment?: string
  created_at: string
  last_used_at?: string
}

export interface CreateSSHKeyInput {
  public_key: string
  comment?: string | undefined
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
  // Primary branch label: the default branch if it contains the commit, else
  // the first branch that does. Only set on the issue-commits + commit-detail
  // responses; absent elsewhere.
  branch?: string
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

export interface RepoTag {
  name: string
  sha: string
  created_at?: string
  message?: string
}

// RefList is a repo's local branches plus the name of the default one.
export interface RefList {
  default: string
  branches: string[]
  tags: RepoTag[]
}

// --- Code review comments ---

// A comment anchored to a line range of a file on a branch. start_line/end_line
// are 1-based and inclusive. snippet is the referenced source lines, filled in
// by the server on list.
export interface CodeComment {
  id: number
  repo_id: number
  ref: string
  path: string
  start_line: number
  end_line: number
  commit_sha?: string
  author: string
  body: string
  resolved: boolean
  snippet?: string
  created_at: string
}

export interface CreateCodeCommentInput {
  ref: string
  path: string
  start_line: number
  end_line: number
  body: string
}

export interface UpdateCodeCommentInput {
  resolved: boolean
}

export type CodeCommentState = "open" | "resolved" | "all"

// --- Pull requests ---

export type PRState = "open" | "merged" | "closed"

export const PR_STATES: readonly PRState[] = ["open", "merged", "closed"] as const

export interface PullRequest {
  id: number
  number: number
  base_ref: string
  head_ref: string
  title: string
  body?: string
  author: string
  state: PRState
  created_at: string
  updated_at: string
  merged_at: string | null
}

// Compare is the three-dot diff of head relative to base: the changes head
// introduces since merge_base(base, head), with ahead/behind counts and the
// base..head commit list. merge_base is "" when the histories are unrelated.
export interface Compare {
  base: string
  head: string
  merge_base: string
  ahead: number
  behind: number
  commits: Commit[]
  files: DiffFile[]
  additions: number
  deletions: number
  truncated: boolean
}

// PullRequestDetail embeds the head-vs-base compare and the review comments
// anchored to the head branch.
export interface PullRequestDetail extends PullRequest {
  compare: Compare
  comments: CodeComment[]
  reviews: PRReview[]
}

export type PRReviewState = "approved" | "changes_requested"

export interface PRReview {
  id: number
  author: string
  state: PRReviewState
  updated_at: string
}

export interface CreatePullRequestInput {
  base: string
  head: string
  title: string
  body?: string | undefined
}

export interface UpdatePullRequestInput {
  title?: string
  body?: string
  state?: PRState
}

export type MergeMethod = "merge" | "ff-only"

export interface MergeRequestInput {
  method?: MergeMethod
}

export interface MergeResult extends PullRequest {
  merge_commit: string
  fast_forward: boolean
}

// MergeConflictResponse is the 409 body when a merge can't proceed: a message
// plus the conflicting paths (absent for non-conflict 409s like "not
// fast-forwardable" or a concurrent base move).
export interface MergeConflictResponse {
  error: string
  conflicts?: string[]
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

// The internal package import DAG dex computed for the repo (its real
// structure), used to rank and layer the Explore package map instead of
// guessing roles from path names. status "no-graph" (with no nodes) means a
// non-Go / un-graphed repo — the UI falls back to the flat summary listing.
export interface IntelPackageGraphNode {
  package: string // full Go import path
  in_degree: number // distinct internal packages importing this one
  out_degree: number // distinct internal packages it imports
  page_rank: number
  is_main?: boolean // executable entry point (Go `package main`); absent from older dex
}

export interface IntelPackageGraphEdge {
  from_package: string
  to_package: string
}

export interface IntelPackageGraph {
  status: string
  hint?: string
  nodes: IntelPackageGraphNode[]
  edges: IntelPackageGraphEdge[]
}

export interface IntelFileSummary {
  path: string
  summary: string
}

// Every dex summary for a repo as a flat path→prose map: "" = repo root,
// directory paths carry their package summary, file paths their file summary.
// One map per repo powers both the breadcrumb (ancestor sub-paths) and the
// file tree (each entry). Paths dex has no prose for are absent.
export interface IntelSummaries {
  summaries: Record<string, string>
}

// --- CI (moongitci) ---

// Mirrors storage.RunStatus. queued -> running -> a terminal state.
export type CIRunStatus =
  | "queued"
  | "running"
  | "awaiting_input" // agent-only: parked between turns
  | "finishing" // agent-only: handing off (branch + summary)
  | "success"
  | "failed"
  | "canceled"
  | "error"
  | "interrupted" // runner went away mid-flight (shutdown/restart) — neutral, not a gate failure
  | "stalled" // agent-only: ran to completion but never converged (max_iterations/no_progress) — neutral, not a failure

// Iteration order for the run status filter chips (Agents views): live states
// first, then terminal — mirrors the lifecycle and keeps the common filters
// (running / awaiting_input) at the front.
export const RUN_STATUSES: readonly CIRunStatus[] = [
  "queued",
  "running",
  "awaiting_input",
  "finishing",
  "success",
  "failed",
  "canceled",
  "error",
  "interrupted",
  "stalled",
]

// Mirrors storage.JobStatus.
export type CIJobStatus =
  | "queued"
  | "running"
  | "success"
  | "failed"
  | "skipped"
  | "error"
  | "interrupted"

// Mirrors storage.RunKind: a CI run, an issue-spawned agent run, or a
// spec-verify agent run (#219). Must list every kind the server can emit.
export type CIRunKind = "ci" | "agent" | "spec-verify"

// Agent execution model (#110): claude-edit is the sole strategy an agent run
// uses in its container — Claude edits files directly. (The mooncake-agent
// alternative, where Claude planned and an external mooncake binary applied
// actions, was removed; the server backfills old runs' execution_model to
// claude-edit, though their stored transcript events still parse as mooncake
// NDJSON — see the folding logic in features/agents/AgentRunBody.tsx.)
export type CIRunExecutionModel = "claude-edit"

// CIRunToolProfile scopes which mgit MCP tools an agent run sees (#184): full =
// the whole toolset; review = read tools + review_* (the read-only review agent).
export type CIRunToolProfile = "full" | "review"

export interface CIRun {
  number: number
  kind: CIRunKind
  issue_number?: number
  execution_model?: CIRunExecutionModel
  tool_profile?: CIRunToolProfile
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

// AgentSettings mirrors api.AgentSettings — the Claude token is write-only (only
// whether one is configured is returned); claude_token_env_fallback reports
// whether a server-env credential (MOONGIT_AGENT_CLAUDE_OAUTH_TOKEN /
// _ANTHROPIC_API_KEY) backs runs when no Settings token is set. llm_base_url
// is the operator-set ANTHROPIC_BASE_URL (not a secret — returned as-is);
// anthropic_auth_token_set reports whether the write-only gateway bearer
// (ANTHROPIC_AUTH_TOKEN) is configured. (execution_model was dropped (#110):
// claude-edit is now the only strategy, so there's nothing left to choose.)
export interface AgentSettings {
  claude_oauth_token_set: boolean
  claude_token_env_fallback: boolean
  llm_base_url?: string
  anthropic_auth_token_set: boolean
}

// UpdateAgentSettingsInput sets global agent config: omit a field to leave it
// unchanged, "" to clear, a value to set.
export interface UpdateAgentSettingsInput {
  claude_oauth_token?: string
  llm_base_url?: string
  anthropic_auth_token?: string
}

export type AgentTurnStatus = "pending" | "running" | "done" | "error"

// AgentTurn is one human follow-up message in an agent run's conversation.
export interface AgentTurn {
  seq: number
  author: string
  body: string
  status: AgentTurnStatus
  created_at: string
  finished_at: string | null
}

export interface CIRunDetail extends CIRun {
  jobs: CIJob[]
  // Follow-up turns for an agent run (issue body is turn 1, not listed).
  turns?: AgentTurn[]
}

// FleetEvent mirrors the api.Event wire type from GET /api/events (SSE fleet
// feed). Seq is monotonic across the server's lifetime; repo is "owner/name"
// (absent for global events); time is unix millis.
export interface FleetEvent {
  seq: number
  type: string
  time: number
  repo?: string
  actor?: string
  data?: Record<string, unknown>
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
  /** Only populated when kind="ask". `answer` is dex's synthesized,
   *  citation-bearing prose response — the headline of the /ask shape;
   *  `answer_model` names the chat model that produced it. Both are absent
   *  when dex's chat leg is unreachable (degrades to the evidence below). */
  answer?: string
  answer_model?: string
  /** Only populated when kind="ask". The CLI prints these and they're more
   *  actionable than the raw semantic_hits. */
  next_action?: string
  avoid?: string
  suggested_reads?: IntelSuggestedRead[]
  annotations?: Record<string, IntelAnnotation>
  graph?: IntelGraph
}

// ── Specs ──────────────────────────────────────────────────────────────────
// In-repo markdown specs under specs/ (what the code *should* do). Mirrors the
// Go api.Spec* wire types.

export type SpecStatus = "living" | "draft" | "superseded"

export interface SpecListItem {
  path: string
  id: string
  title: string
  status?: SpecStatus | string
  owners?: string[]
  covers?: string[]
  last_verified?: string
  /** Verify-agent code↔spec agreement in [0,1]; absent means never verified. */
  alignment?: number
}

export interface SpecList {
  ref: string
  specs: SpecListItem[]
}

export interface SpecSection {
  title: string
  level: number
  body: string
  /** 1-based, file-absolute line of the heading (for deep-links). */
  line: number
}

export interface SpecChecklistItem {
  text: string
  checked: boolean
  line: number
}

export interface SpecContent {
  ref: string
  path: string
  id: string
  title: string
  status?: SpecStatus | string
  owners?: string[]
  covers?: string[]
  last_verified?: string
  alignment?: number
  /** Raw file, frontmatter included (for the editor). */
  content: string
  /** Markdown after the frontmatter (for rendering). */
  body: string
  sections: SpecSection[]
  checklist: SpecChecklistItem[]
  /** The spec's latest verify pass (absent when never verified). */
  verification?: SpecVerification
}

export type SpecMarker = "aligned" | "drifted" | "unverifiable" | "unspecced"

export interface SpecVerificationMarker {
  line: number
  text?: string
  marker: SpecMarker | string
  note?: string
}

export interface SpecVerification {
  alignment: number
  markers: SpecVerificationMarker[]
  conflicts?: string[]
  notes?: string
  verified_at: string
  commit: string
  /** The spec or its governed code changed since this pass — re-verify. */
  stale: boolean
}

export type SpecDriftStatus = "fresh" | "stale" | "unverified" | "uncovered"

export interface SpecDriftItem {
  path: string
  id: string
  status: SpecDriftStatus | string
  covers?: string[]
  base?: string
  changed?: string[]
  last_verified?: string
}

export interface SpecDriftReport {
  ref: string
  specs: SpecDriftItem[]
}

export interface SpecSearchHit {
  path: string
  /** Enclosing spec heading at/before the match line; empty when none. */
  section?: string
  line: number
  snippet?: string
  score: number
}

export interface SpecSearchResult {
  query: string
  hits: SpecSearchHit[]
}

export interface WriteSpecInput {
  content: string
  message?: string
  /** Target branch; defaults server-side to spec/<stem>. Never the default branch. */
  branch?: string
  /** Branch to create the target from when it doesn't exist (default: repo default). */
  base?: string
}

export interface WriteSpecResult {
  branch: string
  commit: string
  /** True when the commit started a new branch (vs. extending one). */
  created: boolean
}
