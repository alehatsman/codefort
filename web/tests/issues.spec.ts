import { expect, test } from "@playwright/test"
import { mockApi, seedToken } from "./mockApi"

test.beforeEach(async ({ page }) => {
  await seedToken(page)
})

test("open the new-issue modal, cancel, no issue is created", async ({ page }) => {
  await mockApi(page)
  await page.goto("/alice/demo/issues")

  await page.getByRole("button", { name: "+ New issue" }).click()
  await expect(page.getByRole("heading", { name: "New issue" })).toBeVisible()

  await page.getByRole("button", { name: "Cancel" }).click()
  await expect(page.getByRole("heading", { name: "New issue" })).not.toBeVisible()

  await expect(page.locator(".issue-row")).toHaveCount(0)
})

test("create an issue via the modal — title + description persist", async ({ page }) => {
  await mockApi(page)
  await page.goto("/alice/demo/issues")

  await page.getByRole("button", { name: "+ New issue" }).click()
  await page.getByLabel("Title").fill("Caching layer needed")
  await page.getByLabel("Description").fill("Add Redis between API and DB")
  await page.getByRole("button", { name: "Submit new issue" }).click()

  // We're redirected to the new issue's detail page; check it rendered.
  await expect(page.getByRole("heading", { name: /Caching layer needed/ })).toBeVisible()
  await expect(page.getByText("Add Redis between API and DB")).toBeVisible()
})

test("create an issue, then it appears in the list", async ({ page }) => {
  await mockApi(page)
  await page.goto("/alice/demo/issues")

  await page.getByRole("button", { name: "+ New issue" }).click()
  await page.getByLabel("Title").fill("First task")
  await page.getByRole("button", { name: "Submit new issue" }).click()

  await page.goto("/alice/demo/issues")
  await expect(page.getByRole("link", { name: /First task/ })).toBeVisible()
})

test("state filter narrows the list", async ({ page }) => {
  await mockApi(page, {
    issues: [
      {
        id: 1,
        number: 1,
        title: "Open one",
        author: "alice",
        state: "todo",
        assignee: null,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      },
      {
        id: 2,
        number: 2,
        title: "Done one",
        author: "alice",
        state: "done",
        assignee: null,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      },
      {
        id: 3,
        number: 3,
        title: "Closed one",
        author: "alice",
        state: "closed",
        assignee: null,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      },
    ],
  })
  await page.goto("/alice/demo/issues")

  // Default filter is todo + in_progress. Closed/done should be hidden.
  await expect(page.locator(".issue-row")).toHaveCount(1)
  await expect(page.getByText("Open one")).toBeVisible()

  // Toggle the closed filter on.
  await page.getByRole("checkbox", { name: "closed" }).check()
  await expect(page.locator(".issue-row")).toHaveCount(2)

  // Assignee = unassigned is a no-op here (all three are unassigned).
  await page.getByLabel("Filter by assignee").selectOption("null")
  await expect(page.locator(".issue-row")).toHaveCount(2)
})

test("author filter and sort control narrow and reorder the list", async ({ page }) => {
  const mk = (n: number, author: string, updated: string) => ({
    id: n,
    number: n,
    title: `Issue ${n}`,
    author,
    state: "todo" as const,
    assignee: null,
    created_at: new Date("2026-01-01").toISOString(),
    updated_at: updated,
  })
  await mockApi(page, {
    issues: [
      mk(1, "alice", "2026-05-03T00:00:00Z"),
      mk(2, "bob", "2026-05-01T00:00:00Z"),
      mk(3, "alice", "2026-05-02T00:00:00Z"),
    ],
  })
  await page.goto("/alice/demo/issues")

  // Default sort is newest (number desc): 3, 1, 2... all three present.
  await expect(page.locator(".issue-row")).toHaveCount(3)
  await expect(page.locator(".issue-row__title")).toHaveText(["Issue 3", "Issue 2", "Issue 1"])

  // Author = bob narrows to just #2 (and persists to the URL).
  await page.getByLabel("Filter by author").selectOption("bob")
  await expect(page.locator(".issue-row")).toHaveCount(1)
  await expect(page.getByText("Issue 2")).toBeVisible()
  await expect(page).toHaveURL(/author=bob/)

  // Back to any; sort oldest reverses to number-ascending order.
  await page.getByLabel("Filter by author").selectOption("")
  await page.getByLabel("Sort issues").selectOption("oldest")
  await expect(page.locator(".issue-row__title")).toHaveText(["Issue 1", "Issue 2", "Issue 3"])

  // Recently-updated orders by updated_at desc: #1 (May 3), #3 (May 2), #2 (May 1).
  await page.getByLabel("Sort issues").selectOption("recently-updated")
  await expect(page.locator(".issue-row__title")).toHaveText(["Issue 1", "Issue 3", "Issue 2"])
  await expect(page).toHaveURL(/sort=recently-updated/)
})

test("search narrows the list by title/body, and #number jumps to the issue", async ({ page }) => {
  await mockApi(page, {
    issues: [
      {
        id: 1,
        number: 1,
        title: "Fix the parser",
        body: "",
        author: "alice",
        state: "todo",
        assignee: null,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      },
      {
        id: 2,
        number: 2,
        title: "Unrelated work",
        body: "tweak the parser internals",
        author: "alice",
        state: "todo",
        assignee: null,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      },
      {
        id: 3,
        number: 3,
        title: "Something else",
        body: "nothing here",
        author: "alice",
        state: "todo",
        assignee: null,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      },
    ],
  })
  await page.goto("/alice/demo/issues")
  await expect(page.locator(".issue-row")).toHaveCount(3)

  // Typing a keyword matches both title (#1) and body (#2), hides #3.
  const search = page.getByRole("searchbox", { name: "Search issues" })
  await search.fill("parser")
  await expect(page.locator(".issue-row")).toHaveCount(2)
  await expect(page.getByText("Fix the parser")).toBeVisible()
  await expect(page.getByText("Unrelated work")).toBeVisible()

  // The query is reflected in the URL so the filtered view is bookmarkable.
  await expect(page).toHaveURL(/[?&]q=parser/)

  // A bare #number is a jump, not a search.
  await search.fill("#3")
  await search.press("Enter")
  await expect(page.getByRole("heading", { name: /Something else/ })).toBeVisible()
})

test("the issue list paginates at 25 per page", async ({ page }) => {
  const now = new Date().toISOString()
  const issues = Array.from({ length: 30 }, (_, i) => ({
    id: i + 1,
    number: i + 1,
    title: `Task ${i + 1}`,
    author: "alice",
    state: "todo" as const,
    assignee: null,
    created_at: now,
    updated_at: now,
  }))
  await mockApi(page, { issues })
  await page.goto("/alice/demo/issues")

  // Page 1: first 25 (newest-first → #30..#6), range + pager reflect the total.
  await expect(page.locator(".issue-row")).toHaveCount(25)
  await expect(page.locator(".pagination__range")).toHaveText("1–25 of 30")

  // Next → page 2 with the remaining 5 (offset=25 on the wire).
  const nextReq = page.waitForRequest((r) => r.url().includes("offset=25"))
  await page.getByRole("button", { name: "Next" }).click()
  await nextReq
  await expect(page.locator(".issue-row")).toHaveCount(5)
  await expect(page.locator(".pagination__range")).toHaveText("26–30 of 30")

  // Prev returns to page 1.
  await page.getByRole("button", { name: "Prev" }).click()
  await expect(page.locator(".issue-row")).toHaveCount(25)

  // Narrowing the filter resets to page 1 (no stranding on an empty page).
  await page.getByRole("button", { name: "Next" }).click()
  await expect(page.locator(".pagination__range")).toHaveText("26–30 of 30")
  await page.getByRole("searchbox", { name: "Search issues" }).fill("Task 7")
  await expect(page.locator(".issue-row")).toHaveCount(1)
})

test("the Board↔List toggle defaults to Board on the left and switches views", async ({ page }) => {
  await mockApi(page, {
    issues: [
      {
        id: 1,
        number: 1,
        title: "Toggle me",
        author: "alice",
        state: "todo",
        assignee: null,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      },
    ],
  })

  // The Issues nav tab points at the board, so that's the default landing.
  await page.goto("/alice/demo/issues/board")
  const sw = page.locator(".view-switch")
  const tabs = sw.getByRole("tab")
  // Board is the first (left) tab and is selected.
  await expect(tabs.first()).toHaveText("Board")
  await expect(tabs.first()).toHaveAttribute("aria-selected", "true")
  await expect(page.getByTestId("board-column-todo")).toBeVisible()

  // Switch to the list.
  await sw.getByRole("tab", { name: "List" }).click()
  await expect(page).toHaveURL(/\/alice\/demo\/issues$/)
  await expect(page.locator(".issue-row", { hasText: "Toggle me" })).toBeVisible()

  // And back to the board.
  await sw.getByRole("tab", { name: "Board" }).click()
  await expect(page).toHaveURL(/\/alice\/demo\/issues\/board$/)
})

test("delete own comment removes it; can't delete others'", async ({ page }) => {
  const now = new Date().toISOString()
  await mockApi(page, {
    issues: [
      {
        id: 1,
        number: 1,
        title: "Discuss design",
        body: "What stack?",
        author: "test-user",
        state: "todo",
        assignee: null,
        created_at: now,
        updated_at: now,
      },
    ],
    comments: [
      { id: 1, issue_id: 1, author: "test-user", body: "Mine to delete", created_at: now },
      { id: 2, issue_id: 1, author: "other-user", body: "Not mine", created_at: now },
    ],
  })
  await page.goto("/alice/demo/issues/1")

  // Confirm both comments are visible.
  await expect(page.getByText("Mine to delete")).toBeVisible()
  await expect(page.getByText("Not mine")).toBeVisible()

  // Two comments, but only ours has a delete button.
  const deleteButtons = page.getByRole("button", { name: "Delete comment" })
  await expect(deleteButtons).toHaveCount(1)

  // Auto-confirm the window.confirm() the component shows.
  page.once("dialog", (d) => d.accept())
  await deleteButtons.click()

  await expect(page.getByText("Mine to delete")).not.toBeVisible()
  await expect(page.getByText("Not mine")).toBeVisible()
})

test("issue body and comments render markdown", async ({ page }) => {
  const now = new Date().toISOString()
  await mockApi(page, {
    issues: [
      {
        id: 1,
        number: 1,
        title: "Rendered",
        body: "## Plan\n\nUse **bold** and `code`.\n\n```\nplain block\nsecond line\n```",
        author: "test-user",
        state: "todo",
        assignee: null,
        created_at: now,
        updated_at: now,
      },
    ],
    comments: [
      {
        id: 1,
        issue_id: 1,
        author: "other-user",
        body: "A [link](https://example.com) here",
        created_at: now,
      },
    ],
  })
  await page.goto("/alice/demo/issues/1")

  // Body markdown: heading, bold, inline code become real elements.
  await expect(page.locator(".body__content h2")).toHaveText("Plan")
  await expect(page.locator(".body__content strong")).toHaveText("bold")
  // Inline code keeps the pill class; a no-language fence renders as a block
  // <pre><code> without it, so it doesn't paint a striped per-line fill.
  await expect(page.locator(".body__content code.md-code-inline")).toHaveText("code")
  await expect(page.locator(".body__content pre code")).toContainText("plain block")
  await expect(page.locator(".body__content pre code.md-code-inline")).toHaveCount(0)

  // Comment markdown: external link renders as an anchor opening in a new tab.
  const link = page.locator(".comment__body a", { hasText: "link" })
  await expect(link).toHaveAttribute("href", "https://example.com")
  await expect(link).toHaveAttribute("target", "_blank")
})

test("Ctrl+Enter in the comment box posts the comment", async ({ page }) => {
  const now = new Date().toISOString()
  await mockApi(page, {
    issues: [
      {
        id: 1,
        number: 1,
        title: "Discuss",
        body: "",
        author: "alice",
        state: "todo",
        assignee: null,
        created_at: now,
        updated_at: now,
      },
    ],
  })
  await page.goto("/alice/demo/issues/1")

  const box = page.getByPlaceholder("Leave a comment")
  await box.fill("posted via keyboard")
  await box.press("Control+Enter")

  await expect(page.getByText("posted via keyboard")).toBeVisible()
})

test("edit an issue's title and body via the UI", async ({ page }) => {
  const now = new Date().toISOString()
  await mockApi(page, {
    issues: [
      {
        id: 1,
        number: 1,
        title: "Old title",
        body: "old body",
        author: "alice",
        state: "todo",
        assignee: null,
        created_at: now,
        updated_at: now,
      },
    ],
  })
  await page.goto("/alice/demo/issues/1")
  await expect(page.getByRole("heading", { name: /Old title/ })).toBeVisible()
  await expect(page.getByText("old body")).toBeVisible()

  // Open the inline editor, change both fields, save.
  await page.getByRole("button", { name: "Edit" }).click()
  await page.getByLabel("Title").fill("New title")
  await page.getByLabel("Description").fill("new body text")
  await page.getByRole("button", { name: "Save" }).click()

  // The page re-renders with the updated content; the editor is gone.
  await expect(page.getByRole("heading", { name: /New title/ })).toBeVisible()
  await expect(page.getByText("new body text")).toBeVisible()
  await expect(page.getByRole("button", { name: "Save" })).toHaveCount(0)
})

test("editing an issue can be cancelled without saving", async ({ page }) => {
  const now = new Date().toISOString()
  await mockApi(page, {
    issues: [
      {
        id: 1,
        number: 1,
        title: "Keep me",
        body: "unchanged",
        author: "alice",
        state: "todo",
        assignee: null,
        created_at: now,
        updated_at: now,
      },
    ],
  })
  await page.goto("/alice/demo/issues/1")

  await page.getByRole("button", { name: "Edit" }).click()
  await page.getByLabel("Title").fill("Discarded edit")
  await page.getByRole("button", { name: "Cancel" }).click()

  // Original title stands; the discarded draft never shows.
  await expect(page.getByRole("heading", { name: /Keep me/ })).toBeVisible()
  await expect(page.getByRole("heading", { name: /Discarded edit/ })).toHaveCount(0)
})

test("issue detail lists the commits referencing it", async ({ page }) => {
  const now = new Date().toISOString()
  await mockApi(page, {
    issues: [
      {
        id: 1,
        number: 1,
        title: "Caching layer",
        body: "",
        author: "alice",
        state: "in_progress",
        assignee: null,
        created_at: now,
        updated_at: now,
      },
    ],
    issueCommits: {
      1: [
        {
          sha: "abc1234def5678abc1234def5678abc1234def56",
          short_sha: "abc1234",
          subject: "feat: add cache (#1)",
          author: "alice",
          email: "a@b.c",
          date: now,
          branch: "main",
        },
        {
          sha: "def5678abc1234def5678abc1234def5678abc12",
          short_sha: "def5678",
          subject: "fix: cache eviction for #1",
          author: "bob",
          email: "b@b.c",
          date: now,
          branch: "feat/cache",
        },
      ],
    },
  })
  await page.goto("/alice/demo/issues/1")

  // The Commits section shows a count and one row per referencing commit,
  // each linking to the commit detail page.
  const section = page.locator(".issue-commits")
  await expect(section.locator(".issue-commits__count")).toHaveText("2")
  await expect(section.locator(".commit-row")).toHaveCount(2)
  await expect(section.getByRole("link", { name: "feat: add cache (#1)" })).toHaveAttribute(
    "href",
    "/alice/demo/commit/abc1234def5678abc1234def5678abc1234def56"
  )
  await expect(section.getByRole("link", { name: "abc1234" })).toBeVisible()
  // Each row carries its primary-branch label.
  await expect(section.locator(".branch-tag")).toHaveText(["main", "feat/cache"])
})

test("issue with no referencing commits hides the Commits section", async ({ page }) => {
  const now = new Date().toISOString()
  await mockApi(page, {
    issues: [
      {
        id: 1,
        number: 1,
        title: "Lonely issue",
        body: "",
        author: "alice",
        state: "todo",
        assignee: null,
        created_at: now,
        updated_at: now,
      },
    ],
  })
  await page.goto("/alice/demo/issues/1")

  await expect(page.getByRole("heading", { name: /Lonely issue/ })).toBeVisible()
  await expect(page.locator(".issue-commits")).toHaveCount(0)
})

test("spawn agent from an issue navigates to the new run", async ({ page }) => {
  const now = new Date().toISOString()
  await mockApi(page, {
    issues: [
      {
        id: 1,
        number: 1,
        title: "Wire the thing",
        author: "test-user",
        state: "todo",
        assignee: null,
        created_at: now,
        updated_at: now,
      },
    ],
  })
  await page.goto("/alice/demo/issues/1")

  // claude-edit is the sole execution model (#110) — nothing to pick, just
  // spawn. The request carries no model/allow_shell.
  const spawnReq = page.waitForRequest(
    (r) => r.url().includes("/issues/1/agent") && r.method() === "POST"
  )
  await page.getByRole("button", { name: "Spawn agent" }).click()
  const body = (await spawnReq).postDataJSON()
  expect(body?.model).toBeUndefined()
  expect(body?.allow_shell).toBeUndefined()

  // Agent runs live under the Agents tab; we land on the new run's view.
  await expect(page).toHaveURL(/\/alice\/demo\/agents\/1$/)
  await expect(page.getByText("Claude (edit)").first()).toBeVisible()
})

test("a root-absolute link in a comment points at the app route, not a blob path", async ({
  page,
}) => {
  const now = new Date().toISOString()
  await mockApi(page, {
    issues: [
      {
        id: 1,
        number: 1,
        title: "Linked",
        author: "test-user",
        state: "todo",
        assignee: null,
        created_at: now,
        updated_at: now,
      },
    ],
    comments: [
      {
        id: 1,
        issue_id: 1,
        author: "moongit-agent",
        body: "Finished — see [run #1](/alice/demo/pipelines/1).",
        created_at: now,
      },
    ],
  })
  await page.goto("/alice/demo/issues/1")

  const link = page.locator(".comment__body a", { hasText: "run #1" })
  // The run link resolves to the app route verbatim — not rewritten under /blob.
  await expect(link).toHaveAttribute("href", "/alice/demo/pipelines/1")
})
