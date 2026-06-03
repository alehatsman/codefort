import { test, expect } from "@playwright/test"
import { mockApi, seedToken } from "./mockApi"

// Per-repo settings (the delete-repo Danger Zone) now live in the global
// /settings page under a "Repositories" section, not a per-repo Settings tab.

test.beforeEach(async ({ page }) => {
  await seedToken(page)
})

test("Repositories section lists repos with a delete affordance", async ({ page }) => {
  await mockApi(page)
  await page.goto("/settings")

  await page.getByRole("button", { name: "Repositories" }).click()
  await expect(page.getByRole("heading", { name: "Repositories" })).toBeVisible()
  await expect(page.getByRole("cell", { name: "alice/demo" })).toBeVisible()
})

test("delete is gated behind type-to-confirm and removes the repo", async ({ page }) => {
  await mockApi(page)
  await page.goto("/settings")
  await page.getByRole("button", { name: "Repositories" }).click()

  // Arm the repo's confirm row.
  await page
    .getByRole("row", { name: /alice\/demo/ })
    .getByRole("button", { name: "Delete" })
    .click()

  const deleteBtn = page.getByRole("button", { name: "Delete repository" })
  const confirm = page.getByLabel("Type the repository name to confirm deletion")

  // Disabled until the exact owner/name slug is typed.
  await expect(deleteBtn).toBeDisabled()
  await confirm.fill("alice/wrong")
  await expect(deleteBtn).toBeDisabled()
  await confirm.fill("alice/demo")
  await expect(deleteBtn).toBeEnabled()

  // Deleting drops the repo from the list, leaving the empty state.
  await deleteBtn.click()
  await expect(page.getByRole("cell", { name: "alice/demo" })).toHaveCount(0)
  await expect(page.getByText("No repositories yet.")).toBeVisible()
})

test("deleting a repo that's already gone surfaces the 404", async ({ page }) => {
  await mockApi(page)
  await page.goto("/settings")
  await page.getByRole("button", { name: "Repositories" }).click()
  await page
    .getByRole("row", { name: /alice\/demo/ })
    .getByRole("button", { name: "Delete" })
    .click()

  const confirm = page.getByLabel("Type the repository name to confirm deletion")
  await confirm.fill("alice/demo")

  // Remove the repo out from under the page so the DELETE hits a 404.
  await page.route(/\/api\/repos\/alice\/demo$/, (route) =>
    route.fulfill({
      status: 404,
      contentType: "application/json",
      body: JSON.stringify({ error: "repo not registered: alice/demo" }),
    })
  )

  await page.getByRole("button", { name: "Delete repository" }).click()
  await expect(page.getByText(/not registered/)).toBeVisible()
})
