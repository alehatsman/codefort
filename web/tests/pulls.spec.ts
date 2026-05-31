import { expect, test } from "@playwright/test"
import type { Page } from "@playwright/test"
import { mockApi, type PullRequest, seedToken } from "./mockApi"

// Pull-request workflow (#70): compare two branches, open a PR, review the
// embedded diff, and merge it (or surface a conflict).

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
  await routeIntelOff(page)

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
      openPR({ id: 2, number: 2, title: "Merged one", state: "merged", merged_at: new Date().toISOString() }),
    ],
  })
  await routeIntelOff(page)

  await page.goto("/alice/demo/pulls")
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

test("merge a pull request from its detail page", async ({ page }) => {
  const state = await mockApi(page, {
    branches: ["main", "feature"],
    pulls: [openPR()],
  })
  await routeIntelOff(page)

  await page.goto("/alice/demo/pulls/1")
  await expect(page.getByRole("heading", { name: /Add feature/ })).toBeVisible()
  // The embedded compare diff renders.
  await expect(page.locator(".diff-file__path")).toContainText("feature.txt")

  await page.getByRole("button", { name: "Merge pull request" }).click()

  // PR flips to merged, server-side and in the badge.
  await expect(page.locator(".pr-state--merged")).toBeVisible()
  expect(state.pulls[0].state).toBe("merged")
})

test("merge surfaces conflicting paths and leaves the PR open", async ({ page }) => {
  const state = await mockApi(page, {
    branches: ["main", "feature"],
    pulls: [openPR()],
    conflictPaths: ["src/app.ts", "README.md"],
  })
  await routeIntelOff(page)

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
  await routeIntelOff(page)

  await page.goto("/alice/demo/pulls/1")
  await page.getByRole("button", { name: "Close" }).click()

  await expect(page.locator(".pr-state--closed")).toBeVisible()
  expect(state.pulls[0].state).toBe("closed")
  // A closed PR offers a Reopen action.
  await expect(page.getByRole("button", { name: "Reopen" })).toBeVisible()
})
