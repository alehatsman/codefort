import { test, expect } from "@playwright/test"
import type { Page } from "@playwright/test"
import { mockApi, seedToken, type State } from "./mockApi"

// CI status badges beside commits (issue #87): the history list, the
// last-commit bars, each link to the commit's run.

const iso = new Date().toISOString()
const sha = "c0ffee00c0ffee00c0ffee00c0ffee00c0ffee00"

// CI on, one commit with a successful run keyed by its SHA.
function ciSeed(ciEnabled = true): Partial<State> {
  return {
    repos: [
      {
        id: 1,
        owner: "alice",
        name: "demo",
        created_at: iso,
        open_issues: 0,
        total_issues: 0,
        ci_enabled: ciEnabled,
      },
    ],
    commits: [
      { sha, short_sha: "c0ffee0", subject: "add CI", author: "alice", email: "a@b.c", date: iso },
    ],
    ciRuns: [
      {
        number: 7,
        commit_sha: sha,
        commit_msg: "add CI",
        commit_author: "alice",
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

function routeBlob(page: Page) {
  return page.route(/\/api\/repos\/[^/]+\/[^/]+\/blob(\?.*)?$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        ref: "main",
        path: "src/app.ts",
        size: 8,
        binary: false,
        too_large: false,
        content: "la1\nla2\n",
      }),
    })
  )
}

// dex off — keep the intel-gated queries quiet in tests.
function routeIntelOff(page: Page) {
  return page.route(/\/api\/repos\/[^/]+\/[^/]+\/intel$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ enabled: false, found: false }),
    })
  )
}

test.beforeEach(async ({ page }) => {
  await seedToken(page)
})

test("commits list shows a CI badge linking to the commit's run", async ({ page }) => {
  await mockApi(page, ciSeed())
  await page.goto("/alice/demo/commits")

  const badge = page.locator("a.commit-ci")
  await expect(badge).toBeVisible()
  await expect(badge).toContainText("success")
  await expect(badge).toHaveAttribute("href", "/alice/demo/pipelines/7")
})

test("no CI badge when the repo has CI disabled", async ({ page }) => {
  // Runs are seeded, but the badge fetch is gated on ci_enabled, so nothing
  // shows.
  await mockApi(page, ciSeed(false))
  await page.goto("/alice/demo/commits")

  await expect(page.getByRole("link", { name: "add CI" })).toBeVisible()
  await expect(page.locator("a.commit-ci")).toHaveCount(0)
})

test("file view header shows a CI badge for the file's last commit", async ({ page }) => {
  await mockApi(page, ciSeed())
  await routeBlob(page)
  await routeIntelOff(page)
  await page.goto("/alice/demo/blob/src/app.ts")

  const badge = page.locator(".commit-meta a.commit-ci")
  await expect(badge).toBeVisible()
  await expect(badge).toHaveAttribute("href", "/alice/demo/pipelines/7")
})
