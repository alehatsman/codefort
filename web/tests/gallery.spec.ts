import { expect, test } from "@playwright/test"
import { mockApi, seedToken } from "./mockApi"

test.beforeEach(async ({ page }) => {
  await seedToken(page)
})

// The /dev/ui gallery is the in-repo showcase for the base UI primitives
// (components/ui). It's a dev tool, so this is a light smoke test: the
// sections render and one interactive primitive actually toggles.
test("dev gallery renders every primitive section", async ({ page }) => {
  await mockApi(page)
  await page.goto("/dev/ui")

  await expect(page.getByRole("heading", { name: "UI primitives" })).toBeVisible()
  for (const section of [
    "Button",
    "Badge",
    "FilterChip",
    "Card",
    "Spinner",
    "EmptyState",
    "ErrorMessage",
    "RelativeTime",
    "Dialog",
  ]) {
    await expect(page.getByRole("heading", { name: section, exact: true })).toBeVisible()
  }

  // The primary button variant is present.
  await expect(page.getByRole("button", { name: "primary", exact: true })).toBeVisible()
})

test("dev gallery Dialog opens and closes", async ({ page }) => {
  await mockApi(page)
  await page.goto("/dev/ui")

  await expect(page.getByRole("dialog")).not.toBeVisible()
  await page.getByRole("button", { name: "Open dialog" }).click()
  await expect(page.getByRole("dialog")).toBeVisible()
  await expect(page.getByRole("heading", { name: "Example dialog" })).toBeVisible()
  await page.getByRole("button", { name: "Cancel" }).click()
  await expect(page.getByRole("dialog")).not.toBeVisible()
})

test("dev gallery FilterChip toggles its checkbox", async ({ page }) => {
  await mockApi(page)
  await page.goto("/dev/ui")

  const todo = page.getByRole("checkbox", { name: "todo" })
  await expect(todo).toBeChecked() // seeded checked in the gallery
  await todo.click()
  await expect(todo).not.toBeChecked()
})
