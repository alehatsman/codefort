import { expect, test } from "@playwright/test"
import { mockApi } from "./mockApi"

// The gate must validate a token against /api/whoami before persisting it, so
// an invalid token never lands in localStorage and traps the user in a broken
// authenticated shell (regression test for the token-gate bug).

test("invalid token is rejected at the gate and never persisted", async ({ page }) => {
  await page.route(/\/api\/whoami$/, (route) =>
    route.fulfill({
      status: 401,
      contentType: "application/json",
      body: JSON.stringify({ error: "invalid token" }),
    })
  )

  await page.goto("/")
  await page.getByPlaceholder("mgt_...").fill("mgt_bogus_qa_token")
  await page.getByRole("button", { name: "Continue" }).click()

  await expect(page.getByText("That token was rejected. Check it and try again.")).toBeVisible()
  // Still on the gate — the app shell never rendered.
  await expect(page.getByRole("button", { name: "Continue" })).toBeVisible()
  await expect(page.getByRole("button", { name: "sign out" })).toHaveCount(0)

  const stored = await page.evaluate(() => localStorage.getItem("codefort_token"))
  expect(stored).toBeNull()
})

test("the token field is focused on load", async ({ page }) => {
  await page.goto("/")
  // autoFocus drops the caret straight into the single field — no click needed.
  await expect(page.getByPlaceholder("mgt_...")).toBeFocused()
})

test("valid token passes the gate, persists, and enters the app", async ({ page }) => {
  await mockApi(page) // mocks /api/whoami -> 200 and /api/repos

  await page.goto("/")
  await page.getByPlaceholder("mgt_...").fill("mgt_valid_qa_token")
  await page.getByRole("button", { name: "Continue" }).click()

  await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible()

  const stored = await page.evaluate(() => localStorage.getItem("codefort_token"))
  expect(stored).toBe("mgt_valid_qa_token")
})
