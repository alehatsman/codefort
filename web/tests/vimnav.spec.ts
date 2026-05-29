import { test, expect } from "@playwright/test"
import { mockApi, seedToken } from "./mockApi"

const now = new Date().toISOString()

test.beforeEach(async ({ page }) => {
  await seedToken(page)
})

test("repos grid: hjkl roves cards, Enter opens the selected repo", async ({ page }) => {
  await mockApi(page, {
    repos: [
      { id: 1, owner: "alice", name: "one", created_at: now, open_issues: 0, total_issues: 0 },
      { id: 2, owner: "alice", name: "two", created_at: now, open_issues: 0, total_issues: 0 },
      { id: 3, owner: "alice", name: "three", created_at: now, open_issues: 0, total_issues: 0 },
    ],
  })
  await page.goto("/")
  await expect(page.locator(".card")).toHaveCount(3)

  // Nothing selected until the first nav key.
  await expect(page.locator(".card.is-vim-selected")).toHaveCount(0)

  // l/j advance the selection; first press selects card 0.
  await page.keyboard.press("l")
  await expect(page.locator(".card").nth(0)).toHaveClass(/is-vim-selected/)
  await page.keyboard.press("l")
  await expect(page.locator(".card").nth(1)).toHaveClass(/is-vim-selected/)
  // k steps back.
  await page.keyboard.press("k")
  await expect(page.locator(".card").nth(0)).toHaveClass(/is-vim-selected/)

  // Enter opens the selected repo.
  await page.keyboard.press("Enter")
  await page.waitForURL("**/alice/one")
})

test("issues list: j/k select a row, Enter opens it; h/l switch tabs", async ({ page }) => {
  await mockApi(page, {
    issues: [
      {
        id: 1,
        number: 1,
        title: "First task",
        author: "alice",
        state: "todo",
        assignee: null,
        created_at: now,
        updated_at: now,
      },
      {
        id: 2,
        number: 2,
        title: "Second task",
        author: "alice",
        state: "todo",
        assignee: null,
        created_at: now,
        updated_at: now,
      },
    ],
  })
  await page.goto("/alice/demo/issues")
  // Newest-first: row 0 is #2, row 1 is #1.
  await expect(page.locator(".issue-row")).toHaveCount(2)

  // j selects the first row, again the second.
  await page.keyboard.press("j")
  await expect(page.locator(".issue-row").nth(0)).toHaveClass(/is-vim-selected/)
  await page.keyboard.press("j")
  await expect(page.locator(".issue-row").nth(1)).toHaveClass(/is-vim-selected/)

  // Enter opens that issue (#1, the older one).
  await page.keyboard.press("Enter")
  await page.waitForURL("**/alice/demo/issues/1")

  // Back on the list, l/h move across the repo tabs. Wait for the tab bar
  // (which carries the h/l handler) to mount on each page before pressing.
  await page.goto("/alice/demo/issues")
  await expect(page.locator(".tabs")).toBeVisible()
  await page.keyboard.press("l")
  await page.waitForURL("**/alice/demo/intel")
  await expect(page.locator(".tabs")).toBeVisible()
  await page.keyboard.press("h")
  await page.waitForURL("**/alice/demo/issues")
  await expect(page.locator(".tabs")).toBeVisible()
  await page.keyboard.press("h")
  await page.waitForURL("**/alice/demo")
})

test("code view: j/k select files, Enter opens, h/l switch tabs", async ({ page }) => {
  await mockApi(page)
  // Tree listing + intel (not indexed, so no overview round trip).
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/tree(\?.*)?$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        ref: "main",
        path: "",
        entries: [
          { name: "src", path: "src", type: "tree" },
          { name: "README.md", path: "README.md", type: "blob", size: 10 },
        ],
      }),
    })
  )
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/intel$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ enabled: false, found: false }),
    })
  )

  await page.goto("/alice/demo")
  await expect(page.locator(".file-tree__row")).toHaveCount(2)

  // j selects the first entry (the src folder).
  await page.keyboard.press("j")
  await expect(page.locator(".file-tree__row").nth(0)).toHaveClass(/is-vim-selected/)

  // Enter opens the folder's tree route.
  await page.keyboard.press("Enter")
  await page.waitForURL("**/alice/demo/tree/src")

  // l/h move across the repo tabs from the code view.
  await page.goto("/alice/demo")
  await expect(page.locator(".file-tree__row").first()).toBeVisible()
  await page.keyboard.press("l")
  await page.waitForURL("**/alice/demo/issues")
})
