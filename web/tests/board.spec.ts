import { test, expect } from "@playwright/test"
import { mockApi, seedToken } from "./mockApi"

test.beforeEach(async ({ page }) => {
  await seedToken(page)
})

test("drag a card from todo to in_progress; state is PATCHed", async ({ page }) => {
  const now = new Date().toISOString()
  const state = await mockApi(page, {
    issues: [
      {
        id: 1, number: 1, title: "Movable task", author: "alice", state: "todo",
        assignee: null, created_at: now, updated_at: now,
      },
    ],
  })

  await page.goto("/alice/demo/issues/board")
  const card = page.locator(`[data-id="issue-1"], .board-card`).first()
  await expect(card).toBeVisible()

  // dnd-kit uses pointer events; dragTo synthesizes them.
  const target = page.getByTestId("board-column-in_progress")
  await card.dragTo(target)

  // Allow the mutation roundtrip + cache invalidation.
  await expect.poll(() => state.issues[0].state).toBe("in_progress")

  // UI eventually shows the card under the new column.
  await expect(target.getByText("Movable task")).toBeVisible()
})
