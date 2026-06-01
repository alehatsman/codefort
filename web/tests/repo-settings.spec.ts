import { test, expect } from "@playwright/test"
import { mockApi, seedToken } from "./mockApi"

test.beforeEach(async ({ page }) => {
  await seedToken(page)
})

test("Settings tab opens the repo Danger Zone", async ({ page }) => {
  await mockApi(page)
  await page.goto("/alice/demo")

  await page.getByRole("link", { name: "Settings", exact: true }).click()
  await expect(page).toHaveURL(/\/alice\/demo\/settings$/)
  await expect(page.getByRole("heading", { name: "Danger Zone" })).toBeVisible()
  await expect(page.getByText("Delete this repository")).toBeVisible()
})

test("delete is gated behind type-to-confirm and removes the repo", async ({ page }) => {
  await mockApi(page)
  await page.goto("/alice/demo/settings")

  const deleteBtn = page.getByRole("button", { name: "Delete repository" })
  const confirm = page.getByLabel("Type the repository name to confirm deletion")

  // Disabled until the exact owner/name slug is typed.
  await expect(deleteBtn).toBeDisabled()
  await confirm.fill("alice/wrong")
  await expect(deleteBtn).toBeDisabled()
  await confirm.fill("alice/demo")
  await expect(deleteBtn).toBeEnabled()

  // Deleting navigates back to the repos list, where the repo is gone.
  await deleteBtn.click()
  await expect(page).toHaveURL(/\/$/)
  await expect(page.getByText("alice/demo")).toHaveCount(0)
})

test("deleting a repo that's already gone surfaces the 404", async ({ page }) => {
  await mockApi(page)
  await page.goto("/alice/demo/settings")

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
  await expect(page).toHaveURL(/\/alice\/demo\/settings$/)
})
