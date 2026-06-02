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

  // The transient selection clears, but the commented range stays tinted so it's
  // visible which lines the thread belongs to (#104).
  await expect(page.locator("#L2")).toHaveClass(/is-commented/)
  await expect(page.locator("#L3")).toHaveClass(/is-commented/)
  await expect(page.locator("#L4")).toHaveClass(/is-commented/)
  await expect(page.locator("#L1")).not.toHaveClass(/is-commented/)
  await expect(page.locator("#L5")).not.toHaveClass(/is-commented/)
})

test("Ctrl+Enter posts the inline code comment", async ({ page }) => {
  const state = await mockApi(page)
  await routeBlob(page)
  await routeIntelOff(page)

  await page.goto("/alice/demo/blob/src/app.ts")
  await expect(page.locator("#L1 .code-line__text")).toContainText("la1")

  // Select line 1, then post with Ctrl+Enter instead of the button.
  await page.locator("#L1 .code-line__num").click()
  const box = page.locator(".code-compose .textarea")
  await box.fill("keyboard-posted review note")
  await box.press("Control+Enter")

  await expect(page.getByText("keyboard-posted review note")).toBeVisible()
  expect(state.codeComments).toHaveLength(1)
  expect(state.codeComments[0]).toMatchObject({ start_line: 1, end_line: 1 })
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

test("review comment bodies render as markdown", async ({ page }) => {
  const seeded: CodeComment[] = [
    {
      id: 1,
      repo_id: 1,
      ref: "main",
      path: "src/app.ts",
      start_line: 1,
      end_line: 1,
      author: "test-user",
      body: "**guard** needed:\n\n- check `nil`\n- return early",
      resolved: false,
      snippet: "la1",
      created_at: new Date().toISOString(),
    },
  ]
  await mockApi(page, { codeComments: seeded })
  await routeBlob(page)
  await routeIntelOff(page)

  await page.goto("/alice/demo/review")

  // The body renders through the shared Markdown component: bold, inline code,
  // and a bullet list — not the raw `**…**`/`-` source text.
  const body = page.locator(".review-row__body .markdown-body")
  await expect(body).toBeVisible()
  await expect(body.locator("strong")).toHaveText("guard")
  await expect(body.locator("code")).toHaveText("nil")
  await expect(body.locator("li")).toHaveCount(2)
  await expect(page.getByText("**guard**")).toHaveCount(0)
})

test("draft review issue spawns a read-only review agent from the Review tab", async ({
  page,
}) => {
  const state = await mockApi(page)
  await routeIntelOff(page)

  await page.goto("/alice/demo/review?ref=main")

  // Open the draft modal.
  await page.getByRole("button", { name: "Draft review issue" }).click()
  const dialog = page.getByRole("dialog")
  await expect(dialog).toBeVisible()

  // Default target is the branch/PR diff → the base ref field is shown, no path.
  await expect(dialog.getByText("Base ref")).toBeVisible()
  await expect(dialog.getByText("File path")).toHaveCount(0)

  // Switch to "A file" → the path field appears and the base field hides.
  await dialog.getByRole("combobox").selectOption("file")
  await expect(dialog.getByText("File path")).toBeVisible()
  await expect(dialog.getByText("Base ref")).toHaveCount(0)

  // The ref seeded from ?ref=main; fill the file path and spawn.
  await dialog.getByPlaceholder("path/to/file.go").fill("src/app.ts")
  await dialog.getByRole("button", { name: "Create + spawn review agent" }).click()

  // It created an issue from the file template and spawned a review-profile agent,
  // then navigated to the agent run's transcript.
  await expect(page).toHaveURL(/\/alice\/demo\/agents\/\d+$/)
  expect(state.issues).toHaveLength(1)
  expect(state.issues[0].title).toBe("Review: src/app.ts")
  expect(state.issues[0].body).toContain("review_create")
  expect(state.ciRuns).toHaveLength(1)
  expect(state.ciRuns[0]).toMatchObject({
    kind: "agent",
    execution_model: "claude-edit",
    tool_profile: "review",
  })
})
