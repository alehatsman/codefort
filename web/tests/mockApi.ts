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

export interface CIJob {
  name: string
  status: string
  exit_code: number | null
  started_at: string | null
  finished_at: string | null
}

export interface CIRun {
  number: number
  commit_sha: string
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

export interface State {
  identity: string
  repos: Repo[]
  issues: Issue[]
  comments: Comment[]
  tokens: Token[]
  ciRuns: CIRun[]
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
    tokens: [{ id: 1, name: "test-user", created_at: nowIso(), last_used_at: nowIso() }],
    ciRuns: [],
    ...seed,
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

function parseQuery(url: URL): { states?: IssueState[]; assignee?: string; limit?: number } {
  const stateRaw = url.searchParams.getAll("state").flatMap((s) => s.split(","))
  const states = stateRaw.filter(Boolean) as IssueState[]
  const assignee = url.searchParams.get("assignee") ?? undefined
  const limitRaw = url.searchParams.get("limit")
  return {
    states: states.length ? states : undefined,
    assignee: assignee ?? undefined,
    limit: limitRaw ? Number(limitRaw) : undefined,
  }
}

function applyIssueFilters(issues: Issue[], q: ReturnType<typeof parseQuery>): Issue[] {
  let out = issues
  if (q.states) out = out.filter((i) => q.states!.includes(i.state))
  if (q.assignee === "null") out = out.filter((i) => i.assignee === null)
  else if (q.assignee) out = out.filter((i) => i.assignee === q.assignee)
  out = [...out].sort((a, b) => b.number - a.number)
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
    if (req.method() === "PATCH") {
      const body = req.postDataJSON() as { ci_enabled?: boolean }
      if (typeof body.ci_enabled === "boolean") repo.ci_enabled = body.ci_enabled
    }
    return json(route, 200, repo)
  })

  // CI: runs list / detail / rerun / events (SSE)
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/ci\/runs(\?.*)?$/, (route) => {
    // Strip jobs from the list view, matching the server's list shape.
    return json(
      route,
      200,
      state.ciRuns.map(({ jobs: _jobs, events: _events, ...run }) => run)
    )
  })
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/ci\/runs\/\d+$/, (route) => {
    const n = Number(new URL(route.request().url()).pathname.split("/").pop())
    const run = state.ciRuns.find((r) => r.number === n)
    if (!run) return json(route, 404, { error: "run not found" })
    const { events: _events, ...detail } = run
    return json(route, 200, detail)
  })
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/ci\/runs\/\d+\/rerun$/, (route) => {
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
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/ci\/runs\/\d+\/jobs\/[^/]+\/events$/, (route) => {
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

  // Whoami
  await page.route(/\/api\/whoami$/, (route) => json(route, 200, { name: state.identity }))

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
      const body = req.postDataJSON() as { state: IssueState }
      iss.state = body.state
      iss.updated_at = nowIso()
      recountRepos(state)
      return json(route, 200, iss)
    }
    return route.continue()
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

  return state
}

/** Set the token in localStorage before app boot so TokenGate doesn't intercept. */
export async function seedToken(page: Page) {
  await page.addInitScript(() => {
    localStorage.setItem("moongit_token", "mgt_test_" + "x".repeat(60))
  })
}
