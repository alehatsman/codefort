import { expect, test } from "@playwright/test"
import { mockApi, seedToken } from "./mockApi"

// The Specs tab lists the repo's in-repo specs (specs/ markdown + parsed
// metadata) and explains the convention when there are none. The list endpoint
// is mocked here; the two-pane render lands in #209.

const SPECS = {
  ref: "main",
  specs: [
    {
      path: "specs/ci/pipeline.md",
      id: "pipeline",
      title: "CI Pipeline",
      status: "draft",
    },
    {
      path: "specs/ssh-transport.md",
      id: "ssh-transport",
      title: "SSH Transport",
      status: "living",
      owners: ["aleh"],
      covers: ["internal/ssh/**"],
      last_verified: "2026-06-02",
      alignment: 0.91,
    },
  ],
}

function mockSpecs(page: import("@playwright/test").Page, body: unknown) {
  return page.route(/\/api\/repos\/[^/]+\/[^/]+\/specs(\?.*)?$/, (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(body) })
  )
}

test("Specs: tab links to /specs and lists specs with status", async ({ page }) => {
  await seedToken(page)
  await mockApi(page)
  await mockSpecs(page, SPECS)

  // Reach the tab from the repo and click through (proves the RepoTabs link).
  await page.goto("/alice/demo")
  await page.getByRole("link", { name: "Specs" }).click()
  await expect(page).toHaveURL(/\/alice\/demo\/specs$/)
  await expect(page.locator(".tab.is-active")).toHaveText("Specs")

  // Both specs render, with their titles and paths.
  const rows = page.locator(".spec-row")
  await expect(rows).toHaveCount(2)
  await expect(rows.filter({ hasText: "SSH Transport" })).toContainText("specs/ssh-transport.md")

  // The living spec carries the living status dot; the draft one the draft dot.
  const living = rows.filter({ hasText: "SSH Transport" })
  await expect(living.locator(".spec-dot--living")).toBeVisible()
  const draft = rows.filter({ hasText: "CI Pipeline" })
  await expect(draft.locator(".spec-dot--draft")).toBeVisible()
})

test("Specs: empty repo shows the convention empty state", async ({ page }) => {
  await seedToken(page)
  await mockApi(page)
  await mockSpecs(page, { ref: "main", specs: [] })

  await page.goto("/alice/demo/specs")

  await expect(page.locator(".spec-row")).toHaveCount(0)
  await expect(page.locator(".empty")).toContainText("No specs yet")
  await expect(page.locator(".empty")).toContainText("docs/specs.md")
})
