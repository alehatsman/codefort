import { test, expect } from "@playwright/test"
import { seedToken } from "./mockApi"

test.beforeEach(async ({ page }) => {
  await seedToken(page)
})

test("GET issues returns 500 → inline error visible", async ({ page }) => {
  // Stub the minimum repo plumbing so the page reaches the issues query.
  await page.route(/\/api\/whoami$/, (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ name: "test-user" }) })
  )
  await page.route(/\/api\/repos$/, (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: "[]" })
  )
  await page.route(/\/api\/repos\/[^/]+\/[^/]+$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        id: 1, owner: "alice", name: "demo",
        created_at: new Date().toISOString(),
        open_issues: 0, total_issues: 0,
      }),
    })
  )
  // The listing endpoint blows up.
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/issues(\?.*)?$/, (route) =>
    route.fulfill({
      status: 500,
      contentType: "application/json",
      body: JSON.stringify({ error: "boom" }),
    })
  )

  await page.goto("/alice/demo/issues")
  await expect(page.locator(".error", { hasText: "boom" })).toBeVisible()
})

test("POST issue returns 500 → modal stays open, error is shown", async ({ page }) => {
  await page.route(/\/api\/whoami$/, (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ name: "test-user" }) })
  )
  await page.route(/\/api\/repos$/, (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: "[]" })
  )
  await page.route(/\/api\/repos\/[^/]+\/[^/]+$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        id: 1, owner: "alice", name: "demo",
        created_at: new Date().toISOString(),
        open_issues: 0, total_issues: 0,
      }),
    })
  )
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/issues(\?.*)?$/, (route) => {
    if (route.request().method() === "GET") {
      return route.fulfill({ status: 200, contentType: "application/json", body: "[]" })
    }
    return route.fulfill({
      status: 500,
      contentType: "application/json",
      body: JSON.stringify({ error: "server kaputt" }),
    })
  })

  await page.goto("/alice/demo/issues")
  await page.getByRole("button", { name: "+ New issue" }).click()
  await page.getByLabel("Title").fill("Will fail")
  await page.getByRole("button", { name: "Submit new issue" }).click()

  // Modal stays open and the error message renders inside it.
  await expect(page.getByRole("heading", { name: "New issue" })).toBeVisible()
  await expect(page.locator(".error", { hasText: "server kaputt" })).toBeVisible()
})
