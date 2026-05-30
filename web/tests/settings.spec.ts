import { test, expect } from "@playwright/test"
import { mockApi, seedToken } from "./mockApi"

test.beforeEach(async ({ page }) => {
  await seedToken(page)
})

test("settings link in the header opens the tokens section", async ({ page }) => {
  await mockApi(page)
  await page.goto("/")

  await page.getByRole("link", { name: "settings" }).click()
  await expect(page).toHaveURL(/\/settings$/)
  await expect(page.getByRole("heading", { name: "API tokens" })).toBeVisible()
  // The seeded token is listed.
  await expect(page.getByRole("cell", { name: "test-user", exact: false })).toBeVisible()
})

test("create a token reveals the secret once", async ({ page }) => {
  await mockApi(page)
  await page.goto("/settings")

  await page.getByPlaceholder("token name").fill("ci-bot")
  await page.getByRole("button", { name: "Create token" }).click()

  // One-time reveal banner with the plaintext secret + copy affordance.
  await expect(page.getByText("New token", { exact: false })).toBeVisible()
  await expect(page.getByText(/^mgt_a+$/)).toBeVisible()
  await expect(page.getByRole("button", { name: "Copy" })).toBeVisible()

  // The new token now appears in the table as active.
  await expect(page.getByRole("cell", { name: "ci-bot", exact: false })).toBeVisible()
})

test("duplicate token name surfaces a 409 error", async ({ page }) => {
  await mockApi(page)
  await page.goto("/settings")

  await page.getByPlaceholder("token name").fill("test-user") // already seeded
  await page.getByRole("button", { name: "Create token" }).click()

  await expect(page.getByText(/already exists/)).toBeVisible()
})

test("revoke a token marks it revoked", async ({ page }) => {
  await mockApi(page)
  // Stub the confirm() dialog so revoke proceeds.
  page.on("dialog", (d) => d.accept())
  await page.goto("/settings")

  await page.getByRole("button", { name: "Revoke" }).first().click()
  await expect(page.getByText("revoked")).toBeVisible()
})

test("placeholder sections are clearly not-yet-available", async ({ page }) => {
  await mockApi(page)
  await page.goto("/settings")

  await page.getByRole("button", { name: "SSH keys" }).click()
  await expect(page.getByText("Coming soon.", { exact: false })).toBeVisible()
})
