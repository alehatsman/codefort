import { expect, test } from "@playwright/test"
import { mockApi, seedToken } from "./mockApi"

// RepoTabs renders a count pill of the work waiting on each tab. The counts are
// route-independent (they live in the top bar), so loading any repo page shows
// them; we use /issues since it's fully mocked by default.

const now = new Date().toISOString()

function run(over: Record<string, unknown>) {
  return {
    number: 0,
    commit_sha: "feedface",
    ref: "refs/heads/main",
    event: "push",
    status: "running",
    created_at: now,
    started_at: now,
    finished_at: null,
    jobs: [],
    ...over,
  }
}

test.beforeEach(async ({ page }) => {
  await seedToken(page)
})

test("repo tabs show count pills, keep Settings, and put Specs second", async ({ page }) => {
  await mockApi(page, {
    // 2 open issues → Issues pill "2".
    issues: [
      {
        id: 1,
        number: 1,
        title: "open one",
        author: "alice",
        state: "todo",
        assignee: null,
        created_at: now,
        updated_at: now,
      },
      {
        id: 2,
        number: 2,
        title: "open two",
        author: "alice",
        state: "in_progress",
        assignee: null,
        created_at: now,
        updated_at: now,
      },
    ],
    // 3 open review comments on the default branch → Review pill "3".
    codeComments: [1, 2, 3].map((i) => ({
      id: i,
      repo_id: 1,
      ref: "main",
      path: "src/a.ts",
      start_line: i,
      end_line: i,
      author: "alice",
      body: "note",
      resolved: false,
      created_at: now,
    })),
    // Pipelines: 2 in-flight ci runs (a terminal one is ignored).
    // Agents: 1 running + 1 awaiting_input = 2 active (a terminal one ignored).
    ciRuns: [
      run({ number: 1, kind: "ci", status: "running" }),
      run({ number: 2, kind: "ci", status: "queued" }),
      run({ number: 3, kind: "ci", status: "success" }),
      run({ number: 4, kind: "agent", status: "running" }),
      run({ number: 5, kind: "agent", status: "awaiting_input" }),
      run({ number: 6, kind: "agent", status: "success" }),
    ],
  })

  // 3 specs → Specs pill "3" (mockApi serves an empty list by default).
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/specs(\?.*)?$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        ref: "main",
        specs: [
          { path: "specs/a.md", id: "a", title: "A" },
          { path: "specs/b.md", id: "b", title: "B" },
          { path: "specs/c.md", id: "c", title: "C" },
        ],
      }),
    })
  )

  await page.goto("/alice/demo/issues")

  const tabs = page.locator(".tabs .tab")

  // Specs is the second tab (right after Code).
  await expect(tabs.nth(0)).toHaveText("Code")
  await expect(tabs.nth(1)).toHaveText(/^Specs/)

  // Count pills.
  await expect(tabs.filter({ hasText: "Specs" }).locator(".tab__count")).toHaveText("3")
  await expect(tabs.filter({ hasText: "Issues" }).locator(".tab__count")).toHaveText("2")
  await expect(tabs.filter({ hasText: "Review" }).locator(".tab__count")).toHaveText("3")
  await expect(tabs.filter({ hasText: "Pipelines" }).locator(".tab__count")).toHaveText("2")
  await expect(tabs.filter({ hasText: "Agents" }).locator(".tab__count")).toHaveText("2")

  // The repo Settings tab came back in fa6d8c0 (visibility, CI, members, danger
  // zone) — it's a per-repo page again, not folded into global /settings.
  await expect(page.getByRole("link", { name: "Settings", exact: true })).toHaveCount(1)
})

test("tabs render no count pill when there's nothing waiting", async ({ page }) => {
  await mockApi(page)
  await page.goto("/alice/demo/issues")

  // Default fixture has no open issues / comments / specs / runs, so no pills.
  await expect(page.locator(".tab__count")).toHaveCount(0)
})
