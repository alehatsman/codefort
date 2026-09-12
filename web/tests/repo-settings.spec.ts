import { expect, test } from "@playwright/test"
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
  // A success toast confirms the deletion (Toast adoption, #309).
  await expect(page.locator(".toast--success")).toContainText("Deleted alice/demo")
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

// The review gate is per-repo and opt-in, so the toggle has to say which state
// it is in — "Require review" reads as an action, not a status.
test("review gate toggles between advisory and required", async ({ page }) => {
  await mockApi(page)
  await page.goto("/alice/demo/settings")

  const row = page.locator(".repo-settings__row", { hasText: "Require review before merge" })
  await expect(row).toContainText("Review verdicts are advisory")

  await row.getByRole("button", { name: "Require review" }).click()
  await expect(row).toContainText("needs one approval")
  await expect(row.getByRole("button", { name: "Make advisory" })).toBeVisible()

  // And back — the setting is a toggle, not a one-way door.
  await row.getByRole("button", { name: "Make advisory" }).click()
  await expect(row).toContainText("Review verdicts are advisory")
})

// The pattern list is submitted whole, and Save stays disabled until the text
// actually differs from what the server holds — otherwise a no-op PATCH is one
// stray click away.
test("protected branches save the pattern list", async ({ page }) => {
  await mockApi(page)
  await page.goto("/alice/demo/settings")

  const row = page.locator(".repo-settings__row", { hasText: "Protected branches" })
  const box = row.getByLabel("Protected branch patterns")
  await expect(box).toHaveValue("")
  await expect(row.getByRole("button", { name: "Save patterns" })).toBeDisabled()

  await box.fill("main\nrelease/*")
  await row.getByRole("button", { name: "Save patterns" }).click()
  await expect(page.getByText("Protecting 2 pattern(s)")).toBeVisible()

  // The saved value round-trips, so Save goes quiet again.
  await expect(box).toHaveValue("main\nrelease/*")
  await expect(row.getByRole("button", { name: "Save patterns" })).toBeDisabled()

  // Clearing it is how a fleet un-protects a branch to rewrite it.
  await box.fill("")
  await row.getByRole("button", { name: "Save patterns" }).click()
  await expect(page.getByText("Branch protection cleared")).toBeVisible()
})
