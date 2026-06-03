import { test, expect } from "@playwright/test"
import { mockApi, seedToken } from "./mockApi"

test.beforeEach(async ({ page }) => {
  await seedToken(page)
})

const iso = new Date().toISOString()

// The repos landing page renders each repo's five at-a-glance metrics as a
// labeled tile grid, in fixed order: CI · Issues · PRs · Reviews · Agents.
test("repo card shows the five metrics in order with deep-links", async ({ page }) => {
  await mockApi(page, {
    repos: [
      {
        id: 1,
        owner: "alice",
        name: "demo",
        created_at: iso,
        open_issues: 3,
        total_issues: 9,
        ci_enabled: true,
        ci_status: "success",
        ci_number: 7,
        open_pulls: 2,
        open_reviews: 5,
        active_agents: 1,
      },
    ],
  })
  await page.goto("/")

  const metrics = page.locator(".repo-metrics").first()
  // Labels appear in the required order.
  await expect(metrics.locator(".repo-metric__label")).toHaveText([
    "CI",
    "Issues",
    "PRs",
    "Reviews",
    "Agents",
  ])

  // Numeric values by tile index — tile 0 is CI (an icon), 1..4 are counts.
  // (The mock recomputes open_issues from the seeded issues array, so issues
  // reads 0 here; PRs/reviews/agents pass through the seed unchanged.)
  const values = metrics.locator(".repo-metric__value")
  await expect(values.nth(2)).toHaveText("2") // PRs
  await expect(values.nth(3)).toHaveText("5") // reviews
  await expect(values.nth(4)).toHaveText("1") // agents

  // Each tile deep-links to the matching repo sub-page; CI links to its run.
  await expect(metrics.locator("a.repo-metric").nth(0)).toHaveAttribute(
    "href",
    "/alice/demo/pipelines/7",
  )
  await expect(metrics.locator("a.repo-metric").nth(1)).toHaveAttribute("href", "/alice/demo/issues")
  await expect(metrics.locator("a.repo-metric").nth(2)).toHaveAttribute("href", "/alice/demo/pulls")
  await expect(metrics.locator("a.repo-metric").nth(3)).toHaveAttribute("href", "/alice/demo/review")
  await expect(metrics.locator("a.repo-metric").nth(4)).toHaveAttribute("href", "/alice/demo/agents")
})

// A repo with no runs links the CI tile to the pipelines index (no run number)
// and dims its zero counts.
test("repo card with no activity links CI to the pipelines index", async ({ page }) => {
  await mockApi(page, {
    repos: [
      {
        id: 1,
        owner: "alice",
        name: "demo",
        created_at: iso,
        open_issues: 0,
        total_issues: 0,
        ci_enabled: false,
        open_pulls: 0,
        open_reviews: 0,
        active_agents: 0,
      },
    ],
  })
  await page.goto("/")

  const metrics = page.locator(".repo-metrics").first()
  await expect(metrics.locator("a.repo-metric").nth(0)).toHaveAttribute(
    "href",
    "/alice/demo/pipelines",
  )
  // Zero counts carry the data-zero marker the dimmed style hangs off.
  await expect(metrics.locator('.repo-metric__value[data-zero="true"]')).toHaveCount(4)
})
