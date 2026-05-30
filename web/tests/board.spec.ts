import { test, expect, type Locator, type Page } from "@playwright/test"
import { mockApi, seedToken } from "./mockApi"

// dnd-kit needs real pointer movement, not a single jump. Press on the card,
// move past the 6px activation constraint, then step onto the target so
// closestCenter resolves the droppable, and release.
async function dragCardOnto(page: Page, card: Locator, target: Locator) {
  const from = await card.boundingBox()
  const to = await target.boundingBox()
  if (!from || !to) throw new Error("card/target not laid out")
  await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2)
  await page.mouse.down()
  await page.mouse.move(from.x + from.width / 2 + 12, from.y + from.height / 2 + 12)
  await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 12 })
  await page.mouse.up()
}

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

  // dnd-kit's PointerSensor activates only after the pointer moves past its 6px
  // constraint, and closestCenter resolves the drop target from intermediate
  // moves. Playwright's dragTo jumps straight to the target in one step, so the
  // drag never activates and `over` stays null — no PATCH. Drive the pointer
  // manually: press, nudge past 6px to activate, then step over the column.
  const target = page.getByTestId("board-column-in_progress")
  await dragCardOnto(page, card, target)

  // Allow the mutation roundtrip + cache invalidation.
  await expect.poll(() => state.issues[0].state).toBe("in_progress")

  // UI eventually shows the card under the new column.
  await expect(target.getByText("Movable task")).toBeVisible()
})
