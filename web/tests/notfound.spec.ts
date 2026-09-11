import { expect, test } from "@playwright/test"
import { mockApi, seedToken } from "./mockApi"

// Unmatched routes and 404s render a styled NotFound page with a link home —
// not a blank page or a raw backend error string.

test.beforeEach(async ({ page }) => {
  await seedToken(page)
})

test("an unmatched top-level route shows the catch-all NotFound", async ({ page }) => {
  await mockApi(page)
  await page.goto("/totally-bogus-page")

  await expect(page.getByRole("heading", { name: "Page not found" })).toBeVisible()
  await expect(page.getByRole("link", { name: /Back to repositories/ })).toBeVisible()
})

test("a non-numeric issue path (e.g. /issues/new) shows NotFound, not a blank", async ({
  page,
}) => {
  await mockApi(page)
  await page.goto("/alice/demo/issues/new")

  await expect(page.getByRole("heading", { name: "Issue not found" })).toBeVisible()
})

test("an unregistered repo (404) shows NotFound, not a raw error string", async ({ page }) => {
  await mockApi(page)
  // Override the repo lookup for this one repo to 404.
  await page.route(/\/api\/repos\/alice\/ghost$/, (route) =>
    route.fulfill({
      status: 404,
      contentType: "application/json",
      body: JSON.stringify({ error: "repo not registered: alice/ghost" }),
    })
  )
  await page.goto("/alice/ghost")

  await expect(page.getByRole("heading", { name: "Repository not found" })).toBeVisible()
  await expect(page.getByText("repo not registered")).toHaveCount(0)
})
