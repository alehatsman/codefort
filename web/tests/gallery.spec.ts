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
    "Avatar",
    "Badge",
    "StatusPill — CI statuses (dense)",
    "ListRow",
    "Table",
    "Sidebar / SidebarSection",
    "Comment",
    "Tooltip",
    "FilterChip",
    "Card",
    "Spinner",
    "EmptyState",
    "ErrorMessage",
    "Skeleton",
    "RelativeTime",
    "Dialog",
  ]) {
    await expect(page.getByRole("heading", { name: section, exact: true })).toBeVisible()
  }

  // The primary button variant is present.
  await expect(page.getByRole("button", { name: "primary", exact: true })).toBeVisible()
})

test("dev gallery Tooltip reveals on focus and links via aria-describedby", async ({ page }) => {
  await mockApi(page)
  await page.goto("/dev/ui")

  // role-based queries skip hidden nodes; use a plain locator so we can assert
  // on the bubble while it's still CSS-hidden.
  const tip = page.locator('[role="tooltip"]').filter({ hasText: "Hover or focus me" })
  await expect(tip).toBeHidden()

  const tipId = await tip.getAttribute("id")
  expect(tipId).toBeTruthy()
  const trigger = page.getByRole("button", { name: "Top (default)" })
  // The trigger is linked to the bubble for screen readers.
  await expect(trigger).toHaveAttribute("aria-describedby", tipId ?? "")

  await trigger.focus()
  await expect(tip).toBeVisible()
  await trigger.blur()
  await expect(tip).toBeHidden()
})

test("dev gallery shows the layout primitives", async ({ page }) => {
  await mockApi(page)
  await page.goto("/dev/ui")

  // PageHeader renders its title as a heading; Toolbar exposes a labeled region.
  await expect(page.getByRole("heading", { name: "Section title", exact: true })).toBeVisible()
  await expect(page.getByRole("toolbar", { name: "Demo toolbar" })).toBeVisible()
})

test("dev gallery form controls — checkbox, radio, switch toggle", async ({ page }) => {
  await mockApi(page)
  await page.goto("/dev/ui")

  // Checkbox (labelled, seeded checked) flips off.
  const check = page.getByRole("checkbox", { name: "Checkbox", exact: true })
  await expect(check).toBeChecked()
  await check.click()
  await expect(check).not.toBeChecked()

  // Radios share a name: picking B deselects A.
  const radioB = page.getByRole("radio", { name: "Radio B" })
  await radioB.check()
  await expect(radioB).toBeChecked()
  await expect(page.getByRole("radio", { name: "Radio A" })).not.toBeChecked()

  // Switch is a checkbox under the hood.
  const toggle = page.getByRole("checkbox", { name: "Switch", exact: true })
  await expect(toggle).not.toBeChecked()
  await toggle.click()
  await expect(toggle).toBeChecked()
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
