import { expect, test } from "@playwright/test"
import { mockApi, seedToken } from "./mockApi"

// The top-level (non-repo) navigation: a global tab bar mirroring the per-repo
// RepoTabs, whose tabs are fleet-wide aggregate views. Each list reuses the
// per-repo row markup but spans every repo, tagging each row with its owning
// repo and linking back into that repo's detail route.

const nowIso = () => new Date().toISOString()

test.beforeEach(async ({ page }) => {
  await seedToken(page)
})

// Mock the three cross-repo aggregate feeds with rows spanning two repos.
async function mockAggregates(page: import("@playwright/test").Page) {
  await page.route(/\/api\/issues(\?.*)?$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify([
        {
          id: 1,
          number: 7,
          title: "alice bug",
          author: "alice",
          state: "todo",
          assignee: null,
          created_at: nowIso(),
          updated_at: nowIso(),
          repo: { owner: "alice", name: "demo" },
        },
        {
          id: 2,
          number: 3,
          title: "bob feature",
          author: "bob",
          state: "in_progress",
          assignee: "bob",
          created_at: nowIso(),
          updated_at: nowIso(),
          repo: { owner: "bob", name: "api" },
        },
      ]),
    })
  )

  await page.route(/\/api\/pulls(\?.*)?$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify([
        {
          id: 1,
          number: 5,
          base_ref: "main",
          head_ref: "feat",
          title: "alice pr",
          author: "alice",
          state: "open",
          created_at: nowIso(),
          updated_at: nowIso(),
          merged_at: null,
          repo: { owner: "alice", name: "demo" },
        },
      ]),
    })
  )

  // Runs feed serves CI runs or agent runs depending on ?kind=.
  await page.route(/\/api\/runs(\?.*)?$/, (route) => {
    const kind = new URL(route.request().url()).searchParams.get("kind")
    const base = {
      commit_sha: "feedface0000abcd",
      commit_msg: "do a thing",
      ref: "refs/heads/main",
      event: "push",
      trigger: "alice",
      status: "success",
      created_at: nowIso(),
      started_at: nowIso(),
      finished_at: nowIso(),
    }
    const rows =
      kind === "agent"
        ? [
            {
              ...base,
              number: 2,
              kind: "agent",
              issue_number: 7,
              execution_model: "claude-edit",
              event: "agent",
              repo: { owner: "bob", name: "api" },
            },
          ]
        : [{ ...base, number: 1, kind: "ci", repo: { owner: "alice", name: "demo" } }]
    return route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(rows) })
  })
}

test("global pulls page shares the per-repo state-filter chips (with icons)", async ({ page }) => {
  await mockApi(page)
  await page.route(/\/api\/pulls(\?.*)?$/, (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: "[]" })
  )

  await page.goto("/pulls")
  // The shared PullsFilters renders FilterChip + PRStateIcon, so each chip
  // carries its glyph here just like the per-repo page — not a bare checkbox.
  await expect(page.locator(".filter-row .chip .state-icon--merged")).toBeVisible()
  await expect(page.locator(".filter-row .chip .state-icon")).toHaveCount(3)
})

test("open a pull request from the global pulls popup", async ({ page }) => {
  const state = await mockApi(page, { branches: ["main", "feature"] })
  await page.route(/\/api\/pulls(\?.*)?$/, (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: "[]" })
  )

  await page.goto("/pulls")

  // Pick the project first, then its branches — the popup loads refs per repo.
  await page.getByRole("button", { name: "+ New pr" }).click()
  await page.getByLabel("Project").selectOption("alice/demo")
  await page.getByLabel("Head branch").selectOption("feature")
  await page.getByLabel("Title").fill("Cross-repo PR")
  await page.getByRole("button", { name: "Create pull request" }).click()

  // Recorded against the chosen repo and lands on its PR detail route.
  await expect(page).toHaveURL(/\/alice\/demo\/pulls\/1$/)
  expect(state.pulls).toHaveLength(1)
  expect(state.pulls[0]).toMatchObject({
    base_ref: "main",
    head_ref: "feature",
    title: "Cross-repo PR",
  })
})

test("global nav links the cross-repo aggregate views", async ({ page }) => {
  await mockApi(page)
  await mockAggregates(page)

  await page.goto("/")
  const nav = page.locator("nav.tabs", { hasText: "Repos" })
  await expect(nav.getByRole("link", { name: "Repos" })).toHaveClass(/is-active/)

  // Issues tab defaults to the board: both repos' issues as cards, each tagged
  // with its repo and linking back into that repo's issue.
  await nav.getByRole("link", { name: "Issues" }).click()
  await expect(page).toHaveURL(/\/issues\/board$/)
  await expect(page.locator(".board-card", { hasText: "alice bug" })).toBeVisible()
  const bobCard = page.locator(".board-card", { hasText: "bob feature" })
  await expect(bobCard.locator(".board-card__repo")).toHaveText("bob/api")
  await expect(bobCard.locator("a.board-card__link")).toHaveAttribute("href", "/bob/api/issues/3")

  // Pull requests tab.
  await page.locator("nav.tabs").getByRole("link", { name: "Pull requests" }).click()
  await expect(page).toHaveURL(/\/pulls$/)
  const prRow = page.locator(".issue-row", { hasText: "alice pr" })
  await expect(prRow.locator(".issue-row__repo")).toHaveText("alice/demo")
  await expect(prRow.getByRole("link")).toHaveAttribute("href", "/alice/demo/pulls/5")

  // Pipelines tab: CI runs across repos, run link into the owning repo.
  await page.locator("nav.tabs").getByRole("link", { name: "Pipelines" }).click()
  await expect(page).toHaveURL(/\/pipelines$/)
  const ciRow = page.locator(".ci-runs tbody tr")
  await expect(ciRow.getByRole("link", { name: "alice/demo" })).toBeVisible()
  await expect(ciRow.getByRole("link", { name: "#1" })).toHaveAttribute(
    "href",
    "/alice/demo/pipelines/1"
  )

  // Agents tab: agent runs across repos, into the owning repo's agents route.
  // The Agents-owned grid is .agent-runs (its own block, not the .ci-runs table).
  await page.locator("nav.tabs").getByRole("link", { name: "Agents" }).click()
  await expect(page).toHaveURL(/\/agents$/)
  const agentRow = page.locator(".agent-runs tbody tr")
  await expect(agentRow.getByRole("link", { name: "bob/api" })).toBeVisible()
  await expect(agentRow.getByRole("link", { name: "#2" })).toHaveAttribute(
    "href",
    "/bob/api/agents/2"
  )
  // The Trigger column surfaces the spawning issue, linked to it.
  await expect(agentRow.getByRole("link", { name: "issue #7" })).toHaveAttribute(
    "href",
    "/bob/api/issues/7"
  )
})

// The global Issues view can create an issue too: the same modal as the
// repo-scoped form, plus a Repository picker that targets which repo it lands
// in. On success we navigate into that repo's new issue.
test("global Issues page creates an issue in the picked repo", async ({ page }) => {
  await mockApi(page) // seeds repo alice/demo + the POST issues handler
  await mockAggregates(page)

  await page.goto("/issues")
  await page.getByRole("button", { name: "+ New issue" }).click()
  await expect(page.getByRole("heading", { name: "New issue" })).toBeVisible()

  // The picker defaults to the only seeded repo.
  await expect(page.getByLabel("Repository")).toHaveValue("alice/demo")

  await page.getByLabel("Title").fill("Cross-repo task")
  await page.getByRole("button", { name: "Submit new issue" }).click()

  // We land on the picked repo's new issue detail page.
  await expect(page).toHaveURL("/alice/demo/issues/1")
  await expect(page.getByRole("heading", { name: /Cross-repo task/ })).toBeVisible()
})
