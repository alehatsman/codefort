import { expect, type Locator, type Page, test } from "@playwright/test"
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
        id: 1,
        number: 1,
        title: "Movable task",
        author: "alice",
        state: "todo",
        assignee: null,
        created_at: now,
        updated_at: now,
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

test("drag a card to done and back to todo; no crash, state PATCHes both ways", async ({
  page,
}) => {
  // Guard the regression behind #148 ("page crashes when moving issue"): a drag
  // to done and a drag back must both succeed without any uncaught page error.
  const pageErrors: string[] = []
  page.on("pageerror", (e) => pageErrors.push(String(e)))

  const now = new Date().toISOString()
  const state = await mockApi(page, {
    issues: [
      {
        id: 1,
        number: 1,
        title: "Round trip",
        author: "alice",
        state: "todo",
        assignee: null,
        created_at: now,
        updated_at: now,
      },
    ],
  })

  await page.goto("/alice/demo/issues/board")
  const done = page.getByTestId("board-column-done")
  const todo = page.getByTestId("board-column-todo")

  // todo -> done
  await dragCardOnto(page, page.locator(".board-card").first(), done)
  await expect.poll(() => state.issues[0].state).toBe("done")
  // Wait for all background refetches, then confirm board is fully re-rendered:
  // card visible in done AND todo column showing empty.
  await page.waitForLoadState("networkidle")
  const doneCard = done.locator(".board-card").first()
  await expect(doneCard).toBeVisible()
  await expect(todo.getByText("No issues")).toBeVisible()

  // done -> todo (the "dragging back" path from #148)
  await dragCardOnto(page, doneCard, todo)
  await expect.poll(() => state.issues[0].state).toBe("todo")
  await expect(todo.getByText("Round trip")).toBeVisible()

  expect(pageErrors, "drag round-trip must not crash the page").toEqual([])
})

test("done column collapses past COL_VISIBLE=30 and expands on demand", async ({ page }) => {
  const base = Date.parse("2026-01-01T00:00:00Z")
  // 33 done issues (> COL_VISIBLE=30) so the collapse toggle is visible.
  // Timestamps ascend so issue #33 (newest) sorts to the top.
  const issues = Array.from({ length: 33 }, (_, i) => {
    const ts = new Date(base + i * 1000).toISOString()
    return {
      id: i + 1,
      number: i + 1,
      title: `Done ${i + 1}`,
      author: "alice",
      state: "done" as const,
      assignee: null,
      created_at: ts,
      updated_at: ts,
    }
  })
  await mockApi(page, { issues })

  await page.goto("/alice/demo/issues/board")
  const done = page.getByTestId("board-column-done")

  // Header always shows the full count.
  await expect(done.locator(".board-col__count")).toHaveText("33")
  // Newest card (#33) is visible near the top.
  await expect(done.getByText("Done 33", { exact: true })).toBeVisible()
  // Oldest 3 cards (#1–#3) are collapsed — not in DOM.
  await expect(done.getByText("Done 1", { exact: true })).toHaveCount(0)

  // Expand reveals all 33; toggle flips to "Show less".
  // The button lives after the virtual container inside board-col__body.
  // Scroll that inner container to the bottom so the button is in the viewport,
  // then use a JS click to bypass any remaining pointer-interception from the
  // virtualiser rows.
  const moreBtn = done.getByRole("button", { name: "Show 3 more" })
  await done.locator(".board-col__body").evaluate((el) => {
    el.scrollTop = el.scrollHeight
  })
  await moreBtn.evaluate((btn) => (btn as HTMLButtonElement).click())
  await expect(done.getByRole("button", { name: "Show less" })).toBeVisible()
})

test("global board groups every repo's issues into state columns", async ({ page }) => {
  const now = new Date().toISOString()
  await page.route(/\/api\/issues(\?.*)?$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify([
        {
          id: 1,
          number: 7,
          title: "alice todo",
          author: "alice",
          state: "todo",
          assignee: null,
          labels: [],
          created_at: now,
          updated_at: now,
          repo: { owner: "alice", name: "demo" },
        },
        {
          id: 2,
          number: 3,
          title: "bob doing",
          author: "bob",
          state: "in_progress",
          assignee: "bob",
          labels: [],
          created_at: now,
          updated_at: now,
          repo: { owner: "bob", name: "api" },
        },
      ]),
    })
  )

  await page.goto("/issues/board")
  const todo = page.getByTestId("board-column-todo")
  const doing = page.getByTestId("board-column-in_progress")

  // Each card lands in its state column, shows its repo, and links back into it.
  const aliceCard = todo.locator(".board-card", { hasText: "alice todo" })
  await expect(aliceCard.locator(".repo-tag")).toHaveText("alice/demo")
  const bobCard = doing.locator(".board-card", { hasText: "bob doing" })
  await expect(bobCard.locator("a.board-card__link")).toHaveAttribute("href", "/bob/api/issues/3")
})
