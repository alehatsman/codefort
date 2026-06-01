import type { Page, Route } from "@playwright/test"

// Wire types — kept independent from src/ so tests have no implicit
// coupling to internal modules and can run with `tsc -b` projects off.
export type IssueState = "todo" | "in_progress" | "done" | "closed"

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

export interface Repo {
  id: number
  owner: string
  name: string
  created_at: string
  open_issues: number
  total_issues: number
  ci_enabled: boolean
}

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

export interface CIJob {
  name: string
  needs?: string[]
  status: string
  exit_code: number | null
  started_at: string | null
  finished_at: string | null
}

export interface AgentTurn {
  seq: number
  author: string
  body: string
  status: "pending" | "running" | "done" | "error"
  created_at: string
  finished_at: string | null
}

export interface CIRun {
  number: number
  kind?: "ci" | "agent"
  issue_number?: number
  execution_model?: "claude-edit" | "mooncake-pilot"
  pilot_allow_shell?: boolean
  turns?: AgentTurn[]
  commit_sha: string
  commit_msg?: string
  commit_author?: string
  ref: string
  event: string
  trigger?: string
  status: string
  created_at: string
  started_at: string | null
  finished_at: string | null
  jobs: CIJob[]
  // events keyed by job name — served (SSE-framed) by the events route.
  events?: Record<
    string,
    { seq: number; type: string; time: number; data?: Record<string, unknown> }[]
  >
}

export interface Token {
  id: number
  name: string
  created_at: string
  last_used_at?: string
  revoked_at?: string
}

export interface SSHKey {
  id: number
  token_name: string
  fingerprint: string
  comment?: string
  created_at: string
  last_used_at?: string
}

export interface Commit {
  sha: string
  short_sha: string
  subject: string
  body?: string
  author: string
  email: string
  date: string
  branch?: string
}

export interface DiffLine {
  kind: "context" | "add" | "del"
  old: number
  new: number
  text: string
}

export interface DiffHunk {
  header: string
  lines: DiffLine[]
}

export interface DiffFile {
  old_path: string
  new_path: string
  status: "added" | "modified" | "deleted" | "renamed"
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

export type PRState = "open" | "merged" | "closed"

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

export interface State {
  identity: string
  repos: Repo[]
  issues: Issue[]
  comments: Comment[]
  codeComments: CodeComment[]
  // Local branches for the /refs endpoint; defaults to ["main"].
  branches: string[]
  tokens: Token[]
  sshKeys: SSHKey[]
  agentClaudeTokenSet: boolean
  agentTokenEnvFallback: boolean
  agentExecutionModel: "" | "claude-edit" | "mooncake-pilot"
  agentLLMBaseURL: string
  agentAuthTokenSet: boolean
  ciRuns: CIRun[]
  // Commit history (newest first) and per-sha diff detail, for the commit
  // diff view. Empty by default; specs that need them seed them.
  commits: Commit[]
  commitDetails: Record<string, CommitDetail>
  // Commits referencing an issue, keyed by issue number. Empty by default;
  // the issue→commits section spec seeds it.
  issueCommits: Record<number, Commit[]>
  // Pull requests + the canned compare returned by /compare and embedded in PR
  // detail (the server computes it from git; the mock serves a fixture).
  pulls: PullRequest[]
  compare: Compare
  // When set, the merge endpoint returns 409 with these conflicting paths
  // instead of merging — lets a spec exercise the conflict surface.
  conflictPaths: string[] | null
}

const OPEN_STATES: IssueState[] = ["todo", "in_progress"]

function nowIso(): string {
  return new Date().toISOString()
}

function freshState(seed: Partial<State> = {}): State {
  return {
    identity: "test-user",
    repos: [
      {
        id: 1,
        owner: "alice",
        name: "demo",
        created_at: nowIso(),
        open_issues: 0,
        total_issues: 0,
        ci_enabled: false,
      },
    ],
    issues: [],
    comments: [],
    codeComments: [],
    branches: ["main"],
    tokens: [{ id: 1, name: "test-user", created_at: nowIso(), last_used_at: nowIso() }],
    sshKeys: [],
    agentClaudeTokenSet: false,
    agentTokenEnvFallback: false,
    agentExecutionModel: "",
    agentLLMBaseURL: "",
    agentAuthTokenSet: false,
    ciRuns: [],
    commits: [],
    commitDetails: {},
    issueCommits: {},
    pulls: [],
    compare: defaultCompare(),
    conflictPaths: null,
    ...seed,
  }
}

// defaultCompare is a small canned three-dot diff: feature is 1 commit / 1 file
// ahead of main, so the compare screen renders a diff and the merge button is
// enabled. Specs needing other shapes seed `compare`.
function defaultCompare(): Compare {
  return {
    base: "main",
    head: "feature",
    merge_base: "0000000000000000000000000000000000000000",
    ahead: 1,
    behind: 0,
    commits: [
      {
        sha: "feature00000000000000000000000000000000a",
        short_sha: "feature0",
        subject: "add feature.txt",
        author: "test-user",
        email: "test@user",
        date: nowIso(),
      },
    ],
    files: [
      {
        old_path: "",
        new_path: "feature.txt",
        status: "added",
        binary: false,
        additions: 1,
        deletions: 0,
        hunks: [
          {
            header: "@@ -0,0 +1 @@",
            lines: [{ kind: "add", old: 0, new: 1, text: "feature" }],
          },
        ],
      },
    ],
    additions: 1,
    deletions: 0,
    truncated: false,
  }
}

function recountRepos(state: State) {
  // Fixtures use a single repo, so all issues belong to it. If multi-
  // repo fixtures are ever needed, key issues by repo and group here.
  for (const r of state.repos) {
    r.total_issues = state.issues.length
    r.open_issues = state.issues.filter((i) => OPEN_STATES.includes(i.state)).length
  }
}

function parseQuery(url: URL): {
  states?: IssueState[]
  assignee?: string
  author?: string
  query?: string
  sort?: string
  limit?: number
} {
  const stateRaw = url.searchParams.getAll("state").flatMap((s) => s.split(","))
  const states = stateRaw.filter(Boolean) as IssueState[]
  const assignee = url.searchParams.get("assignee") ?? undefined
  const author = url.searchParams.get("author") ?? undefined
  const query = url.searchParams.get("q")?.trim() || undefined
  const sort = url.searchParams.get("sort") ?? undefined
  const limitRaw = url.searchParams.get("limit")
  return {
    states: states.length ? states : undefined,
    assignee: assignee ?? undefined,
    author: author ?? undefined,
    query,
    sort: sort ?? undefined,
    limit: limitRaw ? Number(limitRaw) : undefined,
  }
}

function applyIssueFilters(issues: Issue[], q: ReturnType<typeof parseQuery>): Issue[] {
  let out = issues
  if (q.states) out = out.filter((i) => q.states!.includes(i.state))
  if (q.assignee === "null") out = out.filter((i) => i.assignee === null)
  else if (q.assignee) out = out.filter((i) => i.assignee === q.assignee)
  if (q.author) out = out.filter((i) => i.author === q.author)
  if (q.query) {
    const needle = q.query.toLowerCase()
    out = out.filter(
      (i) => i.title.toLowerCase().includes(needle) || (i.body ?? "").toLowerCase().includes(needle)
    )
  }
  out = [...out]
  if (q.sort === "oldest") out.sort((a, b) => a.number - b.number)
  else if (q.sort === "recently-updated")
    out.sort((a, b) => b.updated_at.localeCompare(a.updated_at) || b.number - a.number)
  else out.sort((a, b) => b.number - a.number) // newest (default)
  if (q.limit && q.limit > 0) out = out.slice(0, q.limit)
  return out
}

function json(route: Route, status: number, body: unknown) {
  return route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body),
  })
}

/**
 * Mount mock API handlers on the page. Returns the in-memory state so
 * tests can assert against it directly. Each call is hermetic — fresh
 * state, no leakage between tests.
 */
export async function mockApi(page: Page, seed: Partial<State> = {}): Promise<State> {
  const state = freshState(seed)
  recountRepos(state)

  // Repos
  await page.route(/\/api\/repos$/, (route) => json(route, 200, state.repos))
  await page.route(/\/api\/repos\/[^/]+\/[^/]+$/, (route) => {
    const req = route.request()
    const url = new URL(req.url())
    const [, , , owner, name] = url.pathname.split("/")
    const repo = state.repos.find((r) => r.owner === owner && r.name === name)
    if (!repo) return json(route, 404, { error: "repo not registered: " + owner + "/" + name })
    if (req.method() === "DELETE") {
      state.repos = state.repos.filter((r) => r !== repo)
      recountRepos(state)
      return route.fulfill({ status: 204 })
    }
    if (req.method() === "PATCH") {
      const body = req.postDataJSON() as { ci_enabled?: boolean }
      if (typeof body.ci_enabled === "boolean") repo.ci_enabled = body.ci_enabled
    }
    return json(route, 200, repo)
  })

  // CI: runs list (GET) / manual trigger (POST) / detail / rerun / events (SSE)
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/runs(\?.*)?$/, (route) => {
    const req = route.request()
    if (req.method() === "POST") {
      const ref = ((req.postDataJSON() as { ref?: string }).ref ?? "").trim()
      if (!ref) return json(route, 400, { error: "ref is required" })
      // Mirror the server: only a known ref resolves to a commit; anything
      // else is a 400. (Disabled-repo 409s never reach here — the UI gates
      // the trigger on ci_enabled.)
      if (!state.branches.includes(ref)) {
        return json(route, 400, { error: `cannot resolve ref "${ref}"` })
      }
      const next: CIRun = {
        number: state.ciRuns.length ? Math.max(...state.ciRuns.map((r) => r.number)) + 1 : 1,
        commit_sha: "feedface0000abcd",
        commit_msg: "manual run",
        commit_author: state.identity,
        ref: `refs/heads/${ref}`,
        event: "manual",
        trigger: state.identity,
        status: "queued",
        created_at: nowIso(),
        started_at: null,
        finished_at: null,
        jobs: [],
      }
      state.ciRuns.unshift(next)
      const { jobs: _j, events: _e, ...run } = next
      return json(route, 202, run)
    }
    // GET: strip jobs from the list view, matching the server's list shape;
    // honor the optional ?kind=ci|agent filter (kind defaults to "ci").
    const kind = new URL(route.request().url()).searchParams.get("kind")
    return json(
      route,
      200,
      state.ciRuns
        .filter((r) => !kind || (r.kind ?? "ci") === kind)
        .map(({ jobs: _jobs, events: _events, ...run }) => run)
    )
  })
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/runs\/\d+$/, (route) => {
    const n = Number(new URL(route.request().url()).pathname.split("/").pop())
    const run = state.ciRuns.find((r) => r.number === n)
    if (!run) return json(route, 404, { error: "run not found" })
    const { events: _events, ...detail } = run
    return json(route, 200, detail)
  })
  // Agent follow-up turn: queue a message on an agent run.
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/runs\/\d+\/turns$/, (route) => {
    const parts = new URL(route.request().url()).pathname.split("/")
    const n = Number(parts[parts.length - 2])
    const run = state.ciRuns.find((r) => r.number === n)
    if (!run) return json(route, 404, { error: "run not found" })
    if (run.kind !== "agent") return json(route, 400, { error: "not an agent run" })
    const text = ((route.request().postDataJSON() as { text?: string }).text ?? "").trim()
    if (!text) return json(route, 400, { error: "text is required" })
    run.turns = run.turns ?? []
    const turn: AgentTurn = {
      seq: run.turns.length + 1,
      author: state.identity,
      body: text,
      status: "pending",
      created_at: nowIso(),
      finished_at: null,
    }
    run.turns.push(turn)
    return json(route, 202, turn)
  })
  // Finish an agent run: park -> finishing (the runner would then hand off).
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/runs\/\d+\/finish$/, (route) => {
    const parts = new URL(route.request().url()).pathname.split("/")
    const n = Number(parts[parts.length - 2])
    const run = state.ciRuns.find((r) => r.number === n)
    if (!run) return json(route, 404, { error: "run not found" })
    if (run.kind !== "agent") return json(route, 400, { error: "not an agent run" })
    if (run.status !== "awaiting_input") {
      return json(route, 409, { error: "run is not awaiting input" })
    }
    run.status = "finishing"
    const { jobs: _j, events: _e, ...out } = run
    return json(route, 202, out)
  })
  // Force-stop an agent run from any non-terminal state -> canceled.
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/runs\/\d+\/cancel$/, (route) => {
    const parts = new URL(route.request().url()).pathname.split("/")
    const n = Number(parts[parts.length - 2])
    const run = state.ciRuns.find((r) => r.number === n)
    if (!run) return json(route, 404, { error: "run not found" })
    if (run.kind !== "agent") return json(route, 400, { error: "not an agent run" })
    const terminal = ["success", "failed", "canceled", "error", "interrupted"].includes(run.status)
    if (terminal) return json(route, 409, { error: "run is already finished" })
    run.status = "canceled"
    run.finished_at = new Date().toISOString()
    const { jobs: _j, events: _e, ...out } = run
    return json(route, 202, out)
  })
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/runs\/\d+\/rerun$/, (route) => {
    const parts = new URL(route.request().url()).pathname.split("/")
    const n = Number(parts[parts.length - 2])
    const src = state.ciRuns.find((r) => r.number === n)
    if (!src) return json(route, 404, { error: "run not found" })
    const next: CIRun = {
      ...src,
      number: Math.max(...state.ciRuns.map((r) => r.number)) + 1,
      status: "queued",
      trigger: state.identity,
      started_at: null,
      finished_at: null,
      jobs: [],
    }
    state.ciRuns.push(next)
    const { jobs: _j, events: _e, ...run } = next
    return json(route, 202, run)
  })
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/runs\/\d+\/jobs\/[^/]+\/events$/, (route) => {
    const parts = new URL(route.request().url()).pathname.split("/")
    const job = parts[parts.length - 2]
    const n = Number(parts[parts.length - 4])
    const run = state.ciRuns.find((r) => r.number === n)
    const events = run?.events?.[job] ?? []
    // Frame as SSE: the client reads the full body, then the stream closes.
    const body = events
      .map((ev) => `id: ${ev.seq}\nevent: ${ev.type}\ndata: ${JSON.stringify(ev)}\n\n`)
      .join("")
    return route.fulfill({ status: 200, contentType: "text/event-stream", body })
  })

  // Commit detail (diff). The /commit/{sha} and /commits regexes are disjoint
  // (no "s"), so they don't shadow each other.
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/commit\/[^/]+$/, (route) => {
    const sha = new URL(route.request().url()).pathname.split("/").pop() ?? ""
    const detail = state.commitDetails[sha]
    if (!detail) return json(route, 404, { error: "commit not found: " + sha })
    return json(route, 200, detail)
  })

  // Commit history list.
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/commits(\?.*)?$/, (route) =>
    json(route, 200, { ref: "main", commits: state.commits, has_more: false })
  )

  // Whoami
  await page.route(/\/api\/whoami$/, (route) => json(route, 200, { name: state.identity }))

  // Agent settings — write-only Claude token + default execution model + custom
  // endpoint (shown base URL, write-only gateway auth token). GET reports
  // set/unset + values; PUT sets/clears each provided field.
  await page.route(/\/api\/settings\/agent$/, (route) => {
    const req = route.request()
    if (req.method() === "PUT") {
      const body = req.postDataJSON() as {
        claude_oauth_token?: string
        execution_model?: "" | "claude-edit" | "mooncake-pilot"
        llm_base_url?: string
        anthropic_auth_token?: string
      }
      if (typeof body.claude_oauth_token === "string") {
        state.agentClaudeTokenSet = body.claude_oauth_token.trim() !== ""
      }
      if (typeof body.execution_model === "string") {
        state.agentExecutionModel = body.execution_model
      }
      if (typeof body.llm_base_url === "string") {
        state.agentLLMBaseURL = body.llm_base_url.trim()
      }
      if (typeof body.anthropic_auth_token === "string") {
        state.agentAuthTokenSet = body.anthropic_auth_token.trim() !== ""
      }
    }
    return json(route, 200, {
      claude_oauth_token_set: state.agentClaudeTokenSet,
      claude_token_env_fallback: state.agentTokenEnvFallback,
      execution_model: state.agentExecutionModel,
      llm_base_url: state.agentLLMBaseURL,
      anthropic_auth_token_set: state.agentAuthTokenSet,
    })
  })

  // Tokens collection (GET list / POST create)
  await page.route(/\/api\/tokens$/, async (route) => {
    const req = route.request()
    if (req.method() === "GET") return json(route, 200, state.tokens)
    if (req.method() === "POST") {
      const body = req.postDataJSON() as { name: string }
      if (state.tokens.some((t) => t.name === body.name)) {
        return json(route, 409, { error: "token name already exists: " + body.name })
      }
      const tok: Token = {
        id: state.tokens.length + 1,
        name: body.name,
        created_at: nowIso(),
      }
      state.tokens.push(tok)
      return json(route, 201, { ...tok, secret: "mgt_" + "a".repeat(64) })
    }
    return route.continue()
  })

  // Revoke a token by id
  await page.route(/\/api\/tokens\/\d+$/, async (route) => {
    const req = route.request()
    if (req.method() !== "DELETE") return route.continue()
    const url = new URL(req.url())
    const id = Number(url.pathname.split("/").pop())
    const tok = state.tokens.find((t) => t.id === id)
    if (!tok) return json(route, 404, { error: "token not found" })
    tok.revoked_at = nowIso()
    return route.fulfill({ status: 204 })
  })

  // SSH keys collection (GET list / POST add)
  await page.route(/\/api\/ssh-keys$/, async (route) => {
    const req = route.request()
    if (req.method() === "GET") return json(route, 200, state.sshKeys)
    if (req.method() === "POST") {
      const body = req.postDataJSON() as { public_key: string; comment?: string }
      if (!body.public_key.startsWith("ssh-")) {
        return json(route, 400, { error: "invalid ssh public key" })
      }
      const key: SSHKey = {
        id: state.sshKeys.length + 1,
        token_name: state.identity,
        fingerprint: "SHA256:" + "b".repeat(43),
        comment: body.comment || "",
        created_at: nowIso(),
      }
      state.sshKeys.push(key)
      return json(route, 201, key)
    }
    return route.continue()
  })

  // Delete an SSH key by id
  await page.route(/\/api\/ssh-keys\/\d+$/, async (route) => {
    const req = route.request()
    if (req.method() !== "DELETE") return route.continue()
    const id = Number(new URL(req.url()).pathname.split("/").pop())
    const idx = state.sshKeys.findIndex((k) => k.id === id)
    if (idx === -1) return json(route, 404, { error: "ssh key not found" })
    state.sshKeys.splice(idx, 1)
    return route.fulfill({ status: 204 })
  })

  // Issues collection
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/issues(\?.*)?$/, async (route) => {
    const req = route.request()
    const url = new URL(req.url())
    if (req.method() === "GET") {
      const q = parseQuery(url)
      return json(route, 200, applyIssueFilters(state.issues, q))
    }
    if (req.method() === "POST") {
      const body = req.postDataJSON() as { title: string; body?: string }
      const nextNumber =
        state.issues.length === 0 ? 1 : Math.max(...state.issues.map((i) => i.number)) + 1
      const iss: Issue = {
        id: state.issues.length + 1,
        number: nextNumber,
        title: body.title,
        body: body.body ?? "",
        author: state.identity,
        state: "todo",
        assignee: null,
        created_at: nowIso(),
        updated_at: nowIso(),
      }
      state.issues.push(iss)
      recountRepos(state)
      return json(route, 201, iss)
    }
    return route.continue()
  })

  // One issue (GET / PATCH)
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/issues\/\d+$/, async (route) => {
    const req = route.request()
    const url = new URL(req.url())
    const n = Number(url.pathname.split("/").pop())
    const iss = state.issues.find((i) => i.number === n)
    if (!iss) return json(route, 404, { error: "issue not found" })

    if (req.method() === "GET") return json(route, 200, iss)
    if (req.method() === "PATCH") {
      // Partial update: only the provided fields change (mirrors the server).
      const body = req.postDataJSON() as { state?: IssueState; title?: string; body?: string }
      if (body.state !== undefined) iss.state = body.state
      if (body.title !== undefined) iss.title = body.title
      if (body.body !== undefined) iss.body = body.body
      iss.updated_at = nowIso()
      recountRepos(state)
      return json(route, 200, iss)
    }
    return route.continue()
  })

  // Commits referencing an issue (#n). Disjoint from /issues/{n} and the
  // repo-level /commits route, so it doesn't shadow either.
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/issues\/\d+\/commits$/, (route) => {
    const segs = new URL(route.request().url()).pathname.split("/")
    const n = Number(segs[segs.length - 2])
    return json(route, 200, state.issueCommits[n] ?? [])
  })

  // Claim / unclaim
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/issues\/\d+\/claim$/, async (route) => {
    const url = new URL(route.request().url())
    const n = Number(url.pathname.split("/")[url.pathname.split("/").length - 2])
    const iss = state.issues.find((i) => i.number === n)
    if (!iss) return json(route, 404, { error: "issue not found" })
    if (iss.assignee !== null) return json(route, 409, { error: "issue already claimed" })
    const body = route.request().postDataJSON() as { state?: IssueState }
    iss.assignee = state.identity
    if (body.state) iss.state = body.state
    iss.updated_at = nowIso()
    recountRepos(state)
    return json(route, 200, iss)
  })
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/issues\/\d+\/unclaim$/, async (route) => {
    const url = new URL(route.request().url())
    const n = Number(url.pathname.split("/")[url.pathname.split("/").length - 2])
    const iss = state.issues.find((i) => i.number === n)
    if (!iss) return json(route, 404, { error: "issue not found" })
    iss.assignee = null
    iss.updated_at = nowIso()
    return json(route, 200, iss)
  })

  // Spawn agent — enqueues a kind=agent run linked to the issue, like the
  // server's POST /issues/{n}/agent. Mirrors the trigger mock's run shape.
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/issues\/\d+\/agent$/, (route) => {
    const url = new URL(route.request().url())
    const n = Number(url.pathname.split("/")[url.pathname.split("/").length - 2])
    const iss = state.issues.find((i) => i.number === n)
    if (!iss) return json(route, 404, { error: "issue not found" })
    const body = (route.request().postDataJSON() ?? {}) as {
      model?: "claude-edit" | "mooncake-pilot"
      allow_shell?: boolean
    }
    const model = body.model || state.agentExecutionModel || "claude-edit"
    const next: CIRun = {
      number: state.ciRuns.length ? Math.max(...state.ciRuns.map((r) => r.number)) + 1 : 1,
      kind: "agent",
      issue_number: n,
      execution_model: model,
      pilot_allow_shell: model === "mooncake-pilot" && body.allow_shell === true,
      commit_sha: "feedface0000abcd",
      ref: "HEAD",
      event: "agent",
      trigger: state.identity,
      status: "queued",
      created_at: nowIso(),
      started_at: null,
      finished_at: null,
      jobs: [],
    }
    state.ciRuns.unshift(next)
    const { jobs: _j, events: _e, ...run } = next
    return json(route, 202, run)
  })

  // Comments collection
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/issues\/\d+\/comments$/, async (route) => {
    const req = route.request()
    const url = new URL(req.url())
    const n = Number(url.pathname.split("/")[url.pathname.split("/").length - 2])
    const iss = state.issues.find((i) => i.number === n)
    if (!iss) return json(route, 404, { error: "issue not found" })
    if (req.method() === "GET") {
      return json(
        route,
        200,
        state.comments.filter((c) => c.issue_id === iss.id)
      )
    }
    if (req.method() === "POST") {
      const body = req.postDataJSON() as { body: string }
      const c: Comment = {
        id: state.comments.length + 1,
        issue_id: iss.id,
        author: state.identity,
        body: body.body,
        created_at: nowIso(),
      }
      state.comments.push(c)
      return json(route, 201, c)
    }
    return route.continue()
  })

  // Delete a comment
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/issues\/\d+\/comments\/\d+$/, async (route) => {
    const req = route.request()
    if (req.method() !== "DELETE") return route.continue()
    const url = new URL(req.url())
    const cid = Number(url.pathname.split("/").pop())
    const idx = state.comments.findIndex((c) => c.id === cid)
    if (idx === -1) return json(route, 404, { error: "comment not found" })
    if (state.comments[idx].author !== state.identity) {
      return json(route, 403, { error: "only the author can delete this comment" })
    }
    state.comments.splice(idx, 1)
    return route.fulfill({ status: 204 })
  })

  // Branch list for the selector.
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/refs$/, (route) =>
    json(route, 200, { default: "main", branches: state.branches })
  )

  // Code review comments collection (GET list / POST create). An absent ?ref=
  // means the default branch ("main"), matching the server.
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/code-comments(\?.*)?$/, async (route) => {
    const req = route.request()
    const url = new URL(req.url())
    if (req.method() === "GET") {
      const ref = url.searchParams.get("ref") || "main"
      const path = url.searchParams.get("path") || ""
      const reqState = url.searchParams.get("state") || "open"
      let out = state.codeComments.filter((c) => c.ref === ref)
      if (path) out = out.filter((c) => c.path === path)
      if (reqState === "open") out = out.filter((c) => !c.resolved)
      else if (reqState === "resolved") out = out.filter((c) => c.resolved)
      out = [...out].sort((a, b) =>
        a.path === b.path ? a.start_line - b.start_line : a.path < b.path ? -1 : 1
      )
      return json(route, 200, out)
    }
    if (req.method() === "POST") {
      const body = req.postDataJSON() as {
        ref: string
        path: string
        start_line: number
        end_line: number
        body: string
      }
      const c: CodeComment = {
        id: state.codeComments.length + 1,
        repo_id: 1,
        ref: body.ref || "main",
        path: body.path,
        start_line: body.start_line,
        end_line: body.end_line,
        commit_sha: "deadbeef",
        author: state.identity,
        body: body.body,
        resolved: false,
        snippet: "",
        created_at: nowIso(),
      }
      state.codeComments.push(c)
      return json(route, 201, c)
    }
    return route.continue()
  })

  // One code comment (PATCH resolve / DELETE). Author-only, like the server.
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/code-comments\/\d+$/, async (route) => {
    const req = route.request()
    const url = new URL(req.url())
    const id = Number(url.pathname.split("/").pop())
    const c = state.codeComments.find((x) => x.id === id)
    if (!c) return json(route, 404, { error: "comment not found" })
    if (c.author !== state.identity) {
      return json(route, 403, { error: "only the author can manage this comment" })
    }
    if (req.method() === "PATCH") {
      const body = req.postDataJSON() as { resolved?: boolean }
      if (typeof body.resolved === "boolean") c.resolved = body.resolved
      return json(route, 200, c)
    }
    if (req.method() === "DELETE") {
      state.codeComments.splice(state.codeComments.indexOf(c), 1)
      return route.fulfill({ status: 204 })
    }
    return route.continue()
  })

  // Branch compare: serve the canned fixture, echoing the requested base/head.
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/compare(\?.*)?$/, (route) => {
    const url = new URL(route.request().url())
    const base = url.searchParams.get("base") || "main"
    const head = url.searchParams.get("head") || "feature"
    return json(route, 200, { ...state.compare, base, head })
  })

  // Pull request merge (POST). Registered before the collection/detail routes;
  // its regex is disjoint (ends in /merge) so ordering doesn't actually matter.
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/pulls\/\d+\/merge$/, (route) => {
    const parts = new URL(route.request().url()).pathname.split("/")
    const n = Number(parts[parts.length - 2])
    const pr = state.pulls.find((p) => p.number === n)
    if (!pr) return json(route, 404, { error: "pull request not found" })
    if (pr.state !== "open") return json(route, 409, { error: `pull request is ${pr.state}` })
    if (state.conflictPaths) {
      return json(route, 409, {
        error: "merge conflict; resolve locally and push",
        conflicts: state.conflictPaths,
      })
    }
    const ffOnly = (route.request().postDataJSON() as { method?: string }).method === "ff-only"
    pr.state = "merged"
    pr.merged_at = nowIso()
    pr.updated_at = nowIso()
    return json(route, 200, {
      ...pr,
      merge_commit: "merged00000000000000000000000000000000a",
      fast_forward: ffOnly,
    })
  })

  // One pull request (GET detail / PATCH).
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/pulls\/\d+$/, (route) => {
    const req = route.request()
    const n = Number(new URL(req.url()).pathname.split("/").pop())
    const pr = state.pulls.find((p) => p.number === n)
    if (!pr) return json(route, 404, { error: "pull request not found" })
    if (req.method() === "PATCH") {
      const body = req.postDataJSON() as { title?: string; body?: string; state?: PRState }
      if (body.state === "merged") {
        return json(route, 400, { error: "cannot set state to merged; use the merge endpoint" })
      }
      if (body.title !== undefined) pr.title = body.title
      if (body.body !== undefined) pr.body = body.body
      if (body.state !== undefined) pr.state = body.state
      pr.updated_at = nowIso()
      return json(route, 200, pr)
    }
    // GET detail: embed the compare fixture + review comments on the head ref.
    const comments = state.codeComments.filter((c) => c.ref === pr.head_ref)
    return json(route, 200, { ...pr, compare: state.compare, comments })
  })

  // Pull requests collection (GET list / POST create).
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/pulls(\?.*)?$/, (route) => {
    const req = route.request()
    const url = new URL(req.url())
    if (req.method() === "POST") {
      const body = req.postDataJSON() as { base: string; head: string; title: string; body?: string }
      const number = state.pulls.length ? Math.max(...state.pulls.map((p) => p.number)) + 1 : 1
      const pr: PullRequest = {
        id: state.pulls.length + 1,
        number,
        base_ref: body.base,
        head_ref: body.head,
        title: body.title,
        body: body.body ?? "",
        author: state.identity,
        state: "open",
        created_at: nowIso(),
        updated_at: nowIso(),
        merged_at: null,
      }
      state.pulls.push(pr)
      return json(route, 201, pr)
    }
    // GET: filter by comma-joined / repeated ?state= (absent or empty = any),
    // matching the server's parsePRStates.
    const wants = url.searchParams
      .getAll("state")
      .flatMap((s) => s.split(","))
      .map((s) => s.trim())
      .filter(Boolean)
    const out = wants.length ? state.pulls.filter((p) => wants.includes(p.state)) : state.pulls
    return json(route, 200, [...out].sort((a, b) => b.number - a.number))
  })

  return state
}

/** Set the token in localStorage before app boot so TokenGate doesn't intercept. */
export async function seedToken(page: Page) {
  await page.addInitScript(() => {
    localStorage.setItem("moongit_token", "mgt_test_" + "x".repeat(60))
  })
}
