import { test, expect } from "@playwright/test"
import type { Page } from "@playwright/test"
import { mockApi, seedToken } from "./mockApi"
import type { CodeComment } from "./mockApi"

// Code review comments anchored to file blocks on a branch (issue #55): select
// lines in the blob viewer, leave a comment, and triage them on the Review tab.

const blobBody = JSON.stringify({
  ref: "main",
  path: "src/app.ts",
  size: 20,
  binary: false,
  too_large: false,
  content: "la1\nla2\nla3\nla4\nla5\n",
})

function routeBlob(page: Page) {
  return page.route(/\/api\/repos\/[^/]+\/[^/]+\/blob(\?.*)?$/, (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: blobBody })
  )
}

// dex off — keep the intel-gated queries quiet in tests.
function routeIntelOff(page: Page) {
  return page.route(/\/api\/repos\/[^/]+\/[^/]+\/intel$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ enabled: false, found: false }),
    })
  )
}

test.beforeEach(async ({ page }) => {
  await seedToken(page)
})

test("select a line range in the blob viewer and add an inline comment", async ({ page }) => {
  const state = await mockApi(page)
  await routeBlob(page)
  await routeIntelOff(page)

  await page.goto("/alice/demo/blob/src/app.ts")

  // The code viewer renders the file.
  await expect(page.locator("#L2 .code-line__text")).toContainText("la2")

  // Drag down the gutter from line 2 to line 4: press on L2, move onto L4,
  // release → a 2–4 selection opens the compose form.
  await page.locator("#L2 .code-line__num").hover()
  await page.mouse.down()
  await page.locator("#L4 .code-line__num").hover()
  await page.mouse.up()
  await expect(page.getByText(/Commenting on lines 2.4/)).toBeVisible()

  await page.locator(".code-compose .textarea").fill("this block needs a guard")
  await page.getByRole("button", { name: "Add comment" }).click()

  // The new comment lands inline and is recorded server-side, bound to main.
  await expect(page.getByText("this block needs a guard")).toBeVisible()
  expect(state.codeComments).toHaveLength(1)
  expect(state.codeComments[0]).toMatchObject({
    ref: "main",
    path: "src/app.ts",
    start_line: 2,
    end_line: 4,
    author: "test-user",
  })
})

test("shift-click still extends a selection without dragging", async ({ page }) => {
  await mockApi(page)
  await routeBlob(page)
  await routeIntelOff(page)

  await page.goto("/alice/demo/blob/src/app.ts")
  await expect(page.locator("#L1 .code-line__text")).toContainText("la1")

  // Click line 1, shift-click line 3 → a 1–3 selection.
  await page.locator("#L1 .code-line__num").click()
  await page.locator("#L3 .code-line__num").click({ modifiers: ["Shift"] })
  await expect(page.getByText(/Commenting on lines 1.3/)).toBeVisible()
})

test("review tab groups comments by file, deep-links, and filters by state", async ({ page }) => {
  const seeded: CodeComment[] = [
    {
      id: 1,
      repo_id: 1,
      ref: "main",
      path: "src/app.ts",
      start_line: 2,
      end_line: 3,
      author: "test-user",
      body: "open comment here",
      resolved: false,
      snippet: "la2\nla3",
      created_at: new Date().toISOString(),
    },
    {
      id: 2,
      repo_id: 1,
      ref: "main",
      path: "src/app.ts",
      start_line: 5,
      end_line: 5,
      author: "test-user",
      body: "already done",
      resolved: true,
      snippet: "la5",
      created_at: new Date().toISOString(),
    },
  ]
  const state = await mockApi(page, { codeComments: seeded })
  await routeBlob(page)
  await routeIntelOff(page)

  await page.goto("/alice/demo/review")

  // Default state=open shows only the open comment, grouped under its file.
  await expect(page.getByText("open comment here")).toBeVisible()
  await expect(page.getByText("already done")).toHaveCount(0)

  // Its line range deep-links into the blob viewer at #L2-L3.
  const link = page.getByRole("link", { name: "src/app.ts:L2-L3" })
  await expect(link).toHaveAttribute("href", "/alice/demo/blob/src/app.ts#L2-L3")

  // The snippet is shown for review context.
  await expect(page.locator(".review-row__snippet").first()).toContainText("la2")

  // Also checking "resolved" (open stays checked) → state=all, so the resolved
  // one appears too. One is open (Resolve), one is already resolved (Reopen).
  await page.getByRole("checkbox", { name: "resolved" }).click()
  await expect(page.getByText("already done")).toBeVisible()
  await expect(page.getByRole("button", { name: "Reopen" })).toHaveCount(1)

  // Resolve the open one through the UI (a real PATCH); now both are resolved.
  await page.getByRole("button", { name: "Resolve", exact: true }).click()
  await expect(page.getByRole("button", { name: "Reopen" })).toHaveCount(2)
  expect(state.codeComments.every((c) => c.resolved)).toBe(true)

  // Unchecking "resolved" leaves only the open filter, which is now empty.
  await page.getByRole("checkbox", { name: "resolved" }).click()
  await expect(page.getByText("open comment here")).toHaveCount(0)
})
