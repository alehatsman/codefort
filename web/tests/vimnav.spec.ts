import { test, expect } from "@playwright/test"
import { mockApi, seedToken } from "./mockApi"

const now = new Date().toISOString()

test.beforeEach(async ({ page }) => {
  await seedToken(page)
})

test("repos grid: hjkl moves spatially (j/k a row, h/l a cell), Enter opens", async ({ page }) => {
  // Nine repos; a 1000px viewport renders three 300px columns → a 3×3 grid.
  await mockApi(page, {
    repos: Array.from({ length: 9 }, (_, n) => ({
      id: n + 1,
      owner: "alice",
      name: `r${n + 1}`,
      created_at: now,
      open_issues: 0,
      total_issues: 0,
    })),
  })
  await page.setViewportSize({ width: 1000, height: 900 })
  await page.goto("/")
  await expect(page.locator(".card")).toHaveCount(9)

  // Read the live column count rather than hard-coding the grid math.
  const cols = await page.evaluate(
    () =>
      getComputedStyle(document.querySelector(".card-grid")!)
        .gridTemplateColumns.split(" ")
        .filter(Boolean).length
  )
  expect(cols).toBeGreaterThan(1)

  const selected = page.locator(".card.is-vim-selected")
  // Nothing selected until the first nav key.
  await expect(selected).toHaveCount(0)

  // First key selects card 0.
  await page.keyboard.press("j")
  await expect(page.locator(".card").nth(0)).toHaveClass(/is-vim-selected/)
  // j drops a whole row → first cell of the second row.
  await page.keyboard.press("j")
  await expect(page.locator(".card").nth(cols)).toHaveClass(/is-vim-selected/)
  // l moves one cell right within that row.
  await page.keyboard.press("l")
  await expect(page.locator(".card").nth(cols + 1)).toHaveClass(/is-vim-selected/)
  // k climbs a row back up.
  await page.keyboard.press("k")
  await expect(page.locator(".card").nth(1)).toHaveClass(/is-vim-selected/)
  // h moves one cell left.
  await page.keyboard.press("h")
  await expect(page.locator(".card").nth(0)).toHaveClass(/is-vim-selected/)

  // Enter opens the selected repo.
  await page.keyboard.press("Enter")
  await page.waitForURL("**/alice/r1")
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

  // Back on the list, l/h move across the repo tabs. Between presses wait for
  // the active tab to reflect the destination — that only flips once React
  // has committed the new page (and useTabNav's listener/state with it), so
  // the next keypress can't race an in-flight navigation.
  const activeTab = page.locator(".tab.is-active")
  await page.goto("/alice/demo/issues")
  await expect(activeTab).toHaveText(/Issues/)
  await page.keyboard.press("l")
  await expect(activeTab).toHaveText("Research")
  await page.keyboard.press("h")
  await expect(activeTab).toHaveText(/Issues/)
  await page.keyboard.press("h")
  await expect(activeTab).toHaveText("Code")
  await expect(page).toHaveURL(/\/alice\/demo$/)
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
