import { test, expect } from "@playwright/test"
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
        body: "## Plan\n\nUse **bold** and `code`.",
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
  await expect(page.locator(".body__content code")).toHaveText("code")

  // Comment markdown: external link renders as an anchor opening in a new tab.
  const link = page.locator(".comment__body a", { hasText: "link" })
  await expect(link).toHaveAttribute("href", "https://example.com")
  await expect(link).toHaveAttribute("target", "_blank")
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

  await page.getByRole("button", { name: "Spawn agent" }).click()

  // Agent runs live under the Agents tab; we land on the new run's view.
  await expect(page).toHaveURL(/\/alice\/demo\/agents\/1$/)
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
