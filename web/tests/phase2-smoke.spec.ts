/**
 * Phase 2 smoke tests — run against the real codefortd on :8080.
 * Skips the Vite dev server; hits the production bundle.
 *
 * Run with:
 *   BASE_URL=http://127.0.0.1:8080 npx playwright test tests/phase2-smoke.spec.ts --headed=false
 */
import { expect, test } from "@playwright/test"

const BASE = process.env.BASE_URL ?? "http://127.0.0.1:8080"

// Unique suffix per run so re-runs don't collide on username-taken.
const SUFFIX = Date.now().toString(36)
const USER = `smoketest_${SUFFIX}`
const PASS = "Smoke!Pass123"

test.describe("TokenGate tabs", () => {
  test("shows Use token / Sign in / Create account tabs", async ({ page }) => {
    // Clear any stored token so we land on the gate.
    await page.goto(BASE)
    await page.evaluate(() => localStorage.removeItem("codefort_token"))
    await page.reload()

    await expect(page.getByRole("button", { name: "Use token" })).toBeVisible()
    await expect(page.getByRole("button", { name: "Sign in" })).toBeVisible()
    await expect(page.getByRole("button", { name: "Create account" })).toBeVisible()
  })

  test("Create account tab registers and enters the app", async ({ page }) => {
    await page.goto(BASE)
    await page.evaluate(() => localStorage.removeItem("codefort_token"))
    await page.reload()

    await page.getByRole("button", { name: "Create account" }).click()
    await page.getByPlaceholder("choose a username").fill(USER)
    await page.getByPlaceholder("choose a password").fill(PASS)
    await page.getByRole("button", { name: "Create account" }).last().click()

    // After success the gate should disappear and the app shell appears.
    await expect(page.getByRole("button", { name: "Account menu" })).toBeVisible({ timeout: 5000 })

    const stored = await page.evaluate(() => localStorage.getItem("codefort_token"))
    expect(stored).toMatch(/^mgt_/)
  })

  test("Sign in tab logs in with existing account", async ({ page }) => {
    // Use the account created in the previous test (register it first if needed).
    const loginUser = `login_${SUFFIX}`
    // Pre-register via API so this test is self-contained.
    const regResp = await page.request.post(`${BASE}/api/auth/register`, {
      data: { username: loginUser, password: PASS },
    })
    expect(regResp.ok()).toBeTruthy()

    await page.goto(BASE)
    await page.evaluate(() => localStorage.removeItem("codefort_token"))
    await page.reload()

    await page.getByRole("button", { name: "Sign in" }).click()
    await page.getByPlaceholder("username").fill(loginUser)
    await page.getByPlaceholder("password").fill(PASS)
    await page.getByRole("button", { name: "Sign in" }).last().click()

    await expect(page.getByRole("button", { name: "Account menu" })).toBeVisible({ timeout: 5000 })
  })
})

test.describe("Repo visibility", () => {
  let authToken: string

  test.beforeEach(async ({ page }) => {
    // Register a fresh user and capture the token.
    const r = await page.request.post(`${BASE}/api/auth/register`, {
      data: {
        username: `repovis_${SUFFIX}_${Math.random().toString(36).slice(2, 6)}`,
        password: PASS,
      },
    })
    const body = await r.json()
    authToken = body.secret

    await page.goto(BASE)
    await page.evaluate((tok) => localStorage.setItem("codefort_token", tok), authToken)
    await page.reload()
    // Wait until the app shell is visible.
    await expect(page.getByRole("button", { name: "Account menu" })).toBeVisible({ timeout: 5000 })
  })

  test("New repo form shows visibility selector", async ({ page }) => {
    // Navigate to repos page via topbar link.
    await page.goto(`${BASE}/repos`)
    await page.getByRole("button", { name: "+ New repo" }).click()

    // The dialog should have a visibility field — combobox present and shows public default.
    const combo = page.getByRole("combobox")
    await expect(combo).toBeVisible()
    await expect(combo).toHaveValue("public")
  })

  test("Private repo shows private badge in repos list", async ({ page }) => {
    const owner = await page.evaluate(async (tok) => {
      const r = await fetch("/api/whoami", { headers: { Authorization: `Bearer ${tok}` } })
      const d = await r.json()
      return d.name
    }, authToken)

    // Create a private repo via API.
    const repoName = `priv_${SUFFIX}`
    const cr = await page.request.post(`${BASE}/api/repos`, {
      headers: { Authorization: `Bearer ${authToken}` },
      data: { owner, name: repoName, visibility: "private" },
    })
    expect(cr.ok()).toBeTruthy()

    await page.goto(`${BASE}/repos`)
    // Badge text is exactly "private" (not the option "Private — only collaborators").
    await expect(page.getByText("private", { exact: true })).toBeVisible({ timeout: 5000 })
  })
})
