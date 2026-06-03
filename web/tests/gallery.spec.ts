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
    "Menu",
    "Toast",
    "FilterChip",
    "FormField",
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

test("dev gallery FormField wires label + hint + error to the control", async ({ page }) => {
  await mockApi(page)
  await page.goto("/dev/ui")

  // Label associates by htmlFor/id, and the hint is linked via aria-describedby.
  const named = page.getByLabel("Repository name")
  await expect(named).toBeVisible()
  const hintId = await named.getAttribute("aria-describedby")
  expect(hintId).toBeTruthy()
  await expect(page.locator(`#${hintId}`)).toHaveText("Letters, digits, . _ - only.")

  // The error field renders its inline message and links it for assistive tech.
  const slug = page.getByLabel("Slug")
  await expect(page.getByText("Slug cannot contain spaces.")).toBeVisible()
  await expect(slug).toHaveAttribute("aria-describedby", /-error$/)
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

test("dev gallery Menu opens, keyboard-selects, and dismisses", async ({ page }) => {
  await mockApi(page)
  await page.goto("/dev/ui")

  const trigger = page.getByRole("button", { name: "Row actions" })
  await expect(trigger).toHaveAttribute("aria-expanded", "false")

  // Open → role=menu appears, focus lands on the first item.
  await trigger.click()
  const menu = page.getByRole("menu", { name: "Row actions" })
  await expect(menu).toBeVisible()
  await expect(trigger).toHaveAttribute("aria-expanded", "true")
  await expect(page.getByRole("menuitem", { name: "Edit" })).toBeFocused()

  // Arrow keys rove (skipping the disabled "Archived"); Enter selects + closes.
  await page.keyboard.press("ArrowDown") // Duplicate
  await expect(page.getByRole("menuitem", { name: "Duplicate" })).toBeFocused()
  await page.keyboard.press("ArrowDown") // skips disabled Archived → Delete
  await expect(page.getByRole("menuitem", { name: "Delete" })).toBeFocused()
  await page.keyboard.press("Enter")
  await expect(menu).toBeHidden()
  await expect(page.getByText("chosen: Delete")).toBeVisible()
  await expect(trigger).toBeFocused() // focus returns to the trigger

  // Escape dismisses.
  await trigger.click()
  await expect(menu).toBeVisible()
  await page.keyboard.press("Escape")
  await expect(menu).toBeHidden()

  // Outside-click dismisses.
  await trigger.click()
  await expect(menu).toBeVisible()
  await page.getByRole("heading", { name: "Menu", exact: true }).click()
  await expect(menu).toBeHidden()
})

test("dev gallery Toast appears, stacks, and dismisses", async ({ page }) => {
  await mockApi(page)
  await page.goto("/dev/ui")

  // Fire a success toast (role=status) and an error toast (role=alert).
  await page.getByRole("button", { name: "Success", exact: true }).click()
  const success = page.locator(".toast").filter({ hasText: "Saved your changes" })
  await expect(success).toBeVisible()

  await page.getByRole("button", { name: "Error", exact: true }).click()
  await expect(page.getByRole("alert").filter({ hasText: "Failed to save" })).toBeVisible()
  // Both stack at once.
  await expect(page.locator(".toast")).toHaveCount(2)

  // The × on the success toast dismisses just it.
  await success.getByRole("button", { name: "Dismiss notification" }).click()
  await expect(success).toBeHidden()
  await expect(page.locator(".toast")).toHaveCount(1)
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
