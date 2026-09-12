import { expect, test } from "@playwright/test"
import { mockApi, seedToken } from "./mockApi"

test.beforeEach(async ({ page }) => {
  await seedToken(page)
})

test("settings link in the header opens the tokens section", async ({ page }) => {
  await mockApi(page)
  await page.goto("/")

  await page.getByRole("button", { name: "Account menu" }).click()
  await page.getByRole("menuitem", { name: "settings" }).click()
  await expect(page).toHaveURL(/\/settings$/)
  await expect(page.getByRole("heading", { name: "API tokens" })).toBeVisible()
  // The seeded token is listed.
  await expect(page.getByRole("cell", { name: "test-user", exact: false })).toBeVisible()
})

test("the account menu exposes settings + sign out and dismisses on Escape", async ({ page }) => {
  await mockApi(page)
  await page.goto("/")

  // Collapsed by default: items aren't in the DOM until the trigger opens it.
  const trigger = page.getByRole("button", { name: "Account menu" })
  await expect(trigger).toHaveAttribute("aria-haspopup", "menu")
  await expect(trigger).toHaveAttribute("aria-expanded", "false")
  await expect(page.getByRole("menuitem")).toHaveCount(0)

  await trigger.click()
  await expect(trigger).toHaveAttribute("aria-expanded", "true")
  await expect(page.getByRole("menuitem", { name: "settings" })).toBeVisible()
  await expect(page.getByRole("menuitem", { name: "sign out" })).toBeVisible()

  // Escape dismisses without firing an item and returns focus to the trigger.
  await page.keyboard.press("Escape")
  await expect(page.getByRole("menuitem")).toHaveCount(0)
  await expect(trigger).toBeFocused()
  await expect(page).toHaveURL(/\/$/)
})

test("create a token reveals the secret once", async ({ page }) => {
  await mockApi(page)
  await page.goto("/settings")

  await page.getByPlaceholder("token name").fill("ci-bot")
  await page.getByRole("button", { name: "Create token" }).click()

  // One-time reveal banner with the plaintext secret + copy affordance.
  await expect(page.getByText("New token", { exact: false })).toBeVisible()
  await expect(page.getByText(/^cf_a+$/)).toBeVisible()
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
  await page.goto("/settings")

  await page.getByRole("button", { name: "Revoke" }).first().click()
  // Revoke now opens a confirmation modal; confirm it.
  await page.getByRole("button", { name: "Revoke token" }).click()
  await expect(page.getByText("revoked")).toBeVisible()
})

test("Users section shows account creation form", async ({ page }) => {
  await mockApi(page)
  await page.goto("/settings")

  await page.getByRole("button", { name: "Users" }).click()
  await expect(page.getByRole("heading", { name: "Users" })).toBeVisible()
  await expect(page.getByPlaceholder("username")).toBeVisible()
  await expect(page.getByRole("button", { name: "Create user" })).toBeVisible()
})

// Branch protection shipped as a per-repo setting, so the global surface must
// not offer a section for it — a second place to look is worse than none.
test("global settings has no branch-rules section", async ({ page }) => {
  await mockApi(page)
  await page.goto("/settings")

  await expect(page.getByRole("button", { name: "Branch rules" })).toHaveCount(0)
  await expect(page.getByText("Planned", { exact: false })).toHaveCount(0)
})

test("add an SSH key shows its fingerprint in the list", async ({ page }) => {
  await mockApi(page)
  await page.goto("/settings")

  await page.getByRole("button", { name: "SSH keys" }).click()
  await expect(page.getByRole("heading", { name: "SSH keys" })).toBeVisible()
  await expect(page.getByText("No SSH keys yet.", { exact: false })).toBeVisible()

  await page.getByPlaceholder("ssh-ed25519").fill("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5 laptop")
  await page.getByPlaceholder("label").fill("laptop")
  await page.getByRole("button", { name: "Add SSH key" }).click()

  // The new key appears with its fingerprint and label.
  await expect(page.getByRole("cell", { name: /^SHA256:/ })).toBeVisible()
  await expect(page.getByRole("cell", { name: "laptop", exact: true })).toBeVisible()
})

test("an invalid SSH key surfaces a 400 error", async ({ page }) => {
  await mockApi(page)
  await page.goto("/settings")

  await page.getByRole("button", { name: "SSH keys" }).click()
  await page.getByPlaceholder("ssh-ed25519").fill("not a key")
  await page.getByRole("button", { name: "Add SSH key" }).click()

  await expect(page.getByText(/invalid ssh public key/)).toBeVisible()
})

test("remove an SSH key clears it from the list", async ({ page }) => {
  await mockApi(page, {
    sshKeys: [
      {
        id: 1,
        token_name: "test-user",
        fingerprint: "SHA256:cccccccccccccccccccccccccccccccccccccccccccc",
        comment: "workstation",
        created_at: new Date().toISOString(),
      },
    ],
  })
  await page.goto("/settings")

  await page.getByRole("button", { name: "SSH keys" }).click()
  await expect(page.getByRole("cell", { name: "workstation", exact: true })).toBeVisible()

  await page.getByRole("button", { name: "Remove" }).first().click()
  // Remove now opens a confirmation modal; confirm it.
  await page.getByRole("button", { name: "Remove key" }).click()
  await expect(page.getByText("No SSH keys yet.", { exact: false })).toBeVisible()
})

test("appearance section hosts the theme picker (moved out of the header)", async ({ page }) => {
  await mockApi(page)
  await page.goto("/settings")

  // The picker no longer lives in the topbar.
  await expect(page.getByRole("combobox", { name: "Color scheme" })).toHaveCount(0)

  await page.getByRole("button", { name: "Appearance" }).click()
  await expect(page.getByRole("heading", { name: "Appearance" })).toBeVisible()

  const select = page.getByRole("combobox", { name: "Color scheme" })
  // Default is Monokai (data-theme set on <html>).
  await expect(select).toHaveValue("monokai")
  await expect(page.locator("html")).toHaveAttribute("data-theme", "monokai")

  // Switching to GitHub is the base scheme — the attribute comes off.
  await select.selectOption("github")
  await expect(page.locator("html")).not.toHaveAttribute("data-theme", /.+/)
})

test("agent section sets and clears the global Claude token", async ({ page }) => {
  await mockApi(page)
  await page.goto("/settings")

  await page.getByRole("button", { name: "Agent" }).click()
  await expect(page.getByText(/No Claude token configured/)).toBeVisible()

  await page.getByLabel("Claude token").fill("sk-ant-oat01-secret")
  await page.getByRole("button", { name: "Save token" }).click()

  // Now reports configured; the field is cleared (write-only).
  await expect(page.getByText(/A Claude token is configured/)).toBeVisible()
  await expect(page.getByLabel("Claude token")).toHaveValue("")

  // Clear it.
  await page.getByRole("button", { name: "Clear" }).click()
  await expect(page.getByText(/No Claude token configured/)).toBeVisible()
})

test("agent section reports the env fallback instead of 'no token' (#129)", async ({ page }) => {
  // DB token unset, but the server has CODEFORT_AGENT_CLAUDE_OAUTH_TOKEN /
  // _ANTHROPIC_API_KEY — runs authenticate via the env, so the status must not
  // claim runs "can't authenticate yet".
  await mockApi(page, { agentTokenEnvFallback: true })
  await page.goto("/settings")
  await page.getByRole("button", { name: "Agent" }).click()

  await expect(page.getByText(/Authenticating via the server's environment fallback/)).toBeVisible()
  await expect(page.getByText(/No Claude token configured/)).toHaveCount(0)
})

test("agent section sets a custom endpoint base URL and gateway auth token", async ({ page }) => {
  await mockApi(page)
  await page.goto("/settings")
  await page.getByRole("button", { name: "Agent" }).click()

  // Base URL is a shown (non-secret) field: set it and it persists in place.
  const baseUrl = page.getByLabel("LLM base URL")
  await expect(baseUrl).toHaveValue("")
  await baseUrl.fill("https://gateway.example.com")
  await page.getByRole("button", { name: "Save base URL" }).click()
  await expect(baseUrl).toHaveValue("https://gateway.example.com")

  // The gateway auth token is write-only, mirroring the Claude token.
  await expect(page.getByText(/No gateway auth token/)).toBeVisible()
  await page.getByLabel("Gateway auth token").fill("sk-gw-secret")
  await page.getByRole("button", { name: "Save auth token" }).click()
  await expect(page.getByText(/A gateway auth token is configured/)).toBeVisible()
  await expect(page.getByLabel("Gateway auth token")).toHaveValue("")
})
