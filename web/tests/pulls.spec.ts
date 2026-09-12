import { expect, test } from "@playwright/test"
import { mockApi, type PullRequest, seedToken } from "./mockApi"

// Pull-request workflow (#70): compare two branches, open a PR, review the
// embedded diff, and merge it (or surface a conflict).

function openPR(overrides: Partial<PullRequest> = {}): PullRequest {
  const now = new Date().toISOString()
  return {
    id: 1,
    number: 1,
    base_ref: "main",
    head_ref: "feature",
    title: "Add feature",
    body: "",
    author: "test-user",
    state: "open",
    created_at: now,
    updated_at: now,
    merged_at: null,
    ...overrides,
  }
}

test.beforeEach(async ({ page }) => {
  await seedToken(page)
})

test("compare branches and open a pull request", async ({ page }) => {
  const state = await mockApi(page, { branches: ["main", "feature"] })

  await page.goto("/alice/demo/compare")

  // Choosing a head branch loads the three-dot compare: ahead/behind + diff.
  await page.getByLabel("Head branch").selectOption("feature")
  await expect(page.getByText(/1 commit\(s\) ahead/)).toBeVisible()
  await expect(page.locator(".diff-file__path")).toContainText("feature.txt")

  await page.getByLabel("Pull request title").fill("Add the feature")
  await page.getByRole("button", { name: "Create pull request" }).click()

  // Lands on the new PR's detail page; the PR is recorded server-side.
  await expect(page).toHaveURL(/\/alice\/demo\/pulls\/1$/)
  expect(state.pulls).toHaveLength(1)
  expect(state.pulls[0]).toMatchObject({ base_ref: "main", head_ref: "feature", state: "open" })
})

test("pull request list filters by state", async ({ page }) => {
  await mockApi(page, {
    branches: ["main", "feature"],
    pulls: [
      openPR({ id: 1, number: 1, title: "Open one" }),
      openPR({
        id: 2,
        number: 2,
        title: "Merged one",
        state: "merged",
        merged_at: new Date().toISOString(),
      }),
    ],
  })

  await page.goto("/alice/demo/pulls")
  // Each state chip carries its glyph (open/closed reuse the issue icons,
  // merged has its own).
  await expect(page.locator(".filter-row .chip .state-icon--merged")).toBeVisible()
  // Default view is open PRs only.
  await expect(page.getByText("Open one")).toBeVisible()
  await expect(page.getByText("Merged one")).toBeHidden()

  // Checking "merged" adds it to the default open selection — both show.
  await page.getByRole("checkbox", { name: "merged" }).click()
  await expect(page.getByText("Open one")).toBeVisible()
  await expect(page.getByText("Merged one")).toBeVisible()

  // Unchecking "open" narrows to merged only.
  await page.getByRole("checkbox", { name: "open" }).click()
  await expect(page.getByText("Merged one")).toBeVisible()
  await expect(page.getByText("Open one")).toBeHidden()
})

test("pull request list searches title and body", async ({ page }) => {
  await mockApi(page, {
    branches: ["main", "feature"],
    pulls: [
      openPR({ id: 1, number: 1, title: "Add auth middleware" }),
      openPR({ id: 2, number: 2, title: "Refactor parser" }),
      // Keyword lives only in the body, not the title.
      openPR({ id: 3, number: 3, title: "Tweak config", body: "wires up the auth token" }),
    ],
  })

  await page.goto("/alice/demo/pulls")
  await expect(page.getByText("Add auth middleware")).toBeVisible()
  await expect(page.getByText("Refactor parser")).toBeVisible()

  // Typing a keyword narrows to the title hit and the body-only hit; the
  // parser PR drops out. Debounced into ?q=, so the URL reflects the query.
  const search = page.getByRole("searchbox", { name: "Search pull requests" })
  await search.fill("auth")
  await expect(page.getByText("Add auth middleware")).toBeVisible()
  await expect(page.getByText("Tweak config")).toBeVisible()
  await expect(page.getByText("Refactor parser")).toBeHidden()
  await expect(page).toHaveURL(/[?&]q=auth/)

  // Clearing the box restores the full list and drops the param.
  await search.fill("")
  await expect(page.getByText("Refactor parser")).toBeVisible()
})

test("merge a pull request from its detail page", async ({ page }) => {
  const state = await mockApi(page, {
    branches: ["main", "feature"],
    pulls: [openPR()],
  })

  await page.goto("/alice/demo/pulls/1")
  await expect(page.getByRole("heading", { name: /Add feature/ })).toBeVisible()
  // The embedded compare diff renders.
  await expect(page.locator(".diff-file__path")).toContainText("feature.txt")

  await page.getByRole("button", { name: "Merge pull request" }).click()

  // PR flips to merged, server-side and in the badge.
  await expect(page.locator(".pr-state--merged")).toBeVisible()
  expect(state.pulls[0].state).toBe("merged")
  // A success toast confirms the merge (Toast adoption, #309).
  await expect(page.locator(".toast--success")).toContainText("Pull request #1 merged")
})

test("merge surfaces conflicting paths and leaves the PR open", async ({ page }) => {
  const state = await mockApi(page, {
    branches: ["main", "feature"],
    pulls: [openPR()],
    conflictPaths: ["src/app.ts", "README.md"],
  })

  await page.goto("/alice/demo/pulls/1")
  await page.getByRole("button", { name: "Merge pull request" }).click()

  // Conflict surface lists the paths; the PR stays open.
  await expect(page.getByText(/Merge conflict/)).toBeVisible()
  await expect(page.getByText("src/app.ts")).toBeVisible()
  await expect(page.getByText("README.md")).toBeVisible()
  expect(state.pulls[0].state).toBe("open")
})

test("close a pull request from its detail page", async ({ page }) => {
  const state = await mockApi(page, {
    branches: ["main", "feature"],
    pulls: [openPR()],
  })

  await page.goto("/alice/demo/pulls/1")
  await page.getByRole("button", { name: "Close" }).click()

  await expect(page.locator(".pr-state--closed")).toBeVisible()
  expect(state.pulls[0].state).toBe("closed")
  // A closed PR offers a Reopen action.
  await expect(page.getByRole("button", { name: "Reopen" })).toBeVisible()
})
