import { expect, test } from "@playwright/test"
import { mockApi, type State, seedToken } from "./mockApi"

test.beforeEach(async ({ page }) => {
  await seedToken(page)
})

const iso = new Date().toISOString()

// A repo with three agent runs of different statuses + spawn sources, plus one
// CI run that the Agents tab must never show.
function agentsSeed(): Partial<State> {
  return {
    repos: [
      {
        id: 1,
        owner: "alice",
        name: "demo",
        created_at: iso,
        open_issues: 3,
        total_issues: 3,
        ci_enabled: true,
      },
    ],
    ciRuns: [
      {
        number: 3,
        kind: "agent",
        issue_number: 10,
        tool_profile: "full",
        execution_model: "claude-edit",
        commit_sha: "aaaa111122223333",
        commit_msg: "wire up login",
        commit_author: "Alice Example",
        ref: "HEAD",
        event: "agent",
        trigger: "agent#7",
        status: "running",
        created_at: iso,
        started_at: iso,
        finished_at: null,
        jobs: [],
      },
      {
        number: 2,
        kind: "agent",
        issue_number: 11,
        tool_profile: "review",
        execution_model: "claude-edit",
        commit_sha: "bbbb444455556666",
        commit_msg: "review export path",
        commit_author: "Alice Example",
        ref: "HEAD",
        event: "agent",
        trigger: "agent#8",
        status: "success",
        created_at: iso,
        started_at: iso,
        finished_at: iso,
        jobs: [],
      },
      {
        number: 1,
        kind: "agent",
        issue_number: 12,
        commit_sha: "cccc777788889999",
        commit_msg: "fix parser crash",
        commit_author: "Alice Example",
        ref: "HEAD",
        event: "agent",
        trigger: "agent#9",
        status: "failed",
        created_at: iso,
        started_at: iso,
        finished_at: iso,
        jobs: [],
      },
      {
        number: 99,
        kind: "ci",
        commit_sha: "dddd000011112222",
        commit_msg: "ci run",
        ref: "refs/heads/main",
        event: "push",
        trigger: "alice",
        status: "success",
        created_at: iso,
        started_at: iso,
        finished_at: iso,
        jobs: [],
      },
    ],
  }
}

test("Agents grid shows the new columns and links the spawning issue/review", async ({ page }) => {
  await mockApi(page, agentsSeed())
  await page.goto("/alice/demo/agents")

  // The Agents-owned grid (.agent-runs) renders only the three agent runs.
  const grid = page.locator(".agent-runs")
  await expect(grid).toBeVisible()
  await expect(grid.locator("tbody tr")).toHaveCount(3)
  // The CI run is excluded.
  await expect(grid.getByRole("link", { name: "#99" })).toHaveCount(0)

  // Rows are identified by their run number (the grid has no commit-message
  // column — its columns are Run · Status · Hash · Trigger · Duration · When).
  const running = grid.locator("tbody tr", { has: page.getByRole("link", { name: "#3" }) })
  // Hash column: short SHA links to the commit.
  await expect(running.getByRole("link", { name: "aaaa111" })).toHaveAttribute(
    "href",
    "/alice/demo/commit/aaaa111122223333"
  )
  // Trigger column: a full-profile run reads "issue #N", linked to the issue.
  await expect(running.getByRole("link", { name: "issue #10" })).toHaveAttribute(
    "href",
    "/alice/demo/issues/10"
  )
  // A review-profile run reads "review #N" instead, also linked to the issue.
  const review = grid.locator("tbody tr", { has: page.getByRole("link", { name: "#2" }) })
  await expect(review.getByRole("link", { name: "review #11" })).toHaveAttribute(
    "href",
    "/alice/demo/issues/11"
  )
})

test("status chips filter the Agents grid", async ({ page }) => {
  await mockApi(page, agentsSeed())
  await page.goto("/alice/demo/agents")

  const rows = page.locator(".agent-runs tbody tr")
  await expect(rows).toHaveCount(3)

  // Filtering to "success" narrows to the one finished run (re-queries server-side).
  await page.getByRole("checkbox", { name: "success" }).check()
  await expect(rows).toHaveCount(1)
  await expect(rows.first().getByRole("link", { name: "#2" })).toBeVisible()

  // Adding "failed" widens the set to two.
  await page.getByRole("checkbox", { name: "failed" }).check()
  await expect(rows).toHaveCount(2)

  // Clearing both restores the full list.
  await page.getByRole("checkbox", { name: "success" }).uncheck()
  await page.getByRole("checkbox", { name: "failed" }).uncheck()
  await expect(rows).toHaveCount(3)
})

test("fulltext search filters the Agents grid and is reflected in the URL", async ({ page }) => {
  await mockApi(page, agentsSeed())
  await page.goto("/alice/demo/agents")

  const rows = page.locator(".agent-runs tbody tr")
  await expect(rows).toHaveCount(3)

  // "parser" matches only run #1's commit subject (searched server-side over
  // commit subject/author + ref + trigger).
  await page.getByRole("searchbox", { name: "Search runs" }).fill("parser")
  await expect(page).toHaveURL(/[?&]q=parser/)
  await expect(rows).toHaveCount(1)
  await expect(rows.first().getByRole("link", { name: "issue #12" })).toBeVisible()
})
