import { expect, type Page, test } from "@playwright/test"
import { mockApi, seedToken } from "./mockApi"

// The Specs tab is a two-pane reader: a folder-grouped spec tree + status rail
// on the left, the selected spec rendered as markdown on the right. ↑/↓ move
// between specs and the selection lives in ?path=. The list + content endpoints
// are mocked here.

const SPECS = {
  ref: "main",
  specs: [
    { path: "specs/ci/pipeline.md", id: "pipeline", title: "CI Pipeline", status: "draft" },
    {
      path: "specs/ssh-transport.md",
      id: "ssh-transport",
      title: "SSH Transport",
      status: "living",
      owners: ["aleh"],
      covers: ["internal/ssh/**"],
      last_verified: "2026-06-02",
      alignment: 0.91,
    },
  ],
}

// Spec content keyed by path relative to specs/ (the content route's {path...}).
const CONTENT: Record<string, { title: string; status: string; body: string; meta?: object }> = {
  "ci/pipeline.md": {
    title: "CI Pipeline",
    status: "draft",
    body: "## Intent\nThe CI DAG: jobs wired by needs.",
  },
  "ssh-transport.md": {
    title: "SSH Transport",
    status: "living",
    body: "## Intent\nGit over SSH as an opt-in second port.",
    meta: { owners: ["aleh"], covers: ["internal/ssh/**"], last_verified: "2026-06-02", alignment: 0.91 },
  },
}

async function mockSpecs(page: Page, list: unknown) {
  // Content endpoint (.../specs/<path>) — registered first; its regex requires
  // a trailing segment so it's disjoint from the list route below.
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/specs\/[^?]+/, (route) => {
    const rel = decodeURIComponent(new URL(route.request().url()).pathname.split("/specs/")[1])
    const c = CONTENT[rel]
    if (!c) return route.fulfill({ status: 404, body: "not found" })
    return route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        ref: "main",
        path: `specs/${rel}`,
        id: rel.replace(/\.md$/, ""),
        title: c.title,
        status: c.status,
        content: `# ${c.title}\n${c.body}`,
        body: `# ${c.title}\n${c.body}`,
        sections: [],
        checklist: [],
        ...c.meta,
      }),
    })
  })
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/specs(\?.*)?$/, (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(list) })
  )
}

test("Specs: tab links to /specs; two-pane tree + status rail + rendered spec", async ({
  page,
}) => {
  await seedToken(page)
  await mockApi(page)
  await mockSpecs(page, SPECS)

  // Reach the tab from the repo and click through (proves the RepoTabs link).
  await page.goto("/alice/demo")
  await page.getByRole("link", { name: "Specs" }).click()
  await expect(page).toHaveURL(/\/alice\/demo\/specs/)
  await expect(page.locator(".tab.is-active")).toHaveText("Specs")

  // Status rail counts the lifecycle states (1 living, 1 draft).
  const rail = page.locator(".spec-rail")
  await expect(rail).toContainText("1 living")
  await expect(rail).toContainText("1 draft")

  // Tree: both specs, with the "ci" subfolder grouped under its header.
  await expect(page.locator(".spec-tree__item")).toHaveCount(2)
  await expect(page.locator(".spec-tree__folder")).toHaveText("ci")

  // Default selection (first in tree order = the root-group spec) renders in
  // the center pane as markdown.
  await expect(page.locator(".spec-view__title")).toHaveText("SSH Transport")
  await expect(page.locator(".spec-view")).toContainText("opt-in second port")
  // The living spec's metadata line surfaces owners/covers/verification.
  await expect(page.locator(".spec-view__meta")).toContainText("owners: aleh")
  await expect(page.locator(".spec-view__meta")).toContainText("91% aligned")
})

test("Specs: clicking a spec selects it, renders it, and reflects in ?path", async ({ page }) => {
  await seedToken(page)
  await mockApi(page)
  await mockSpecs(page, SPECS)

  await page.goto("/alice/demo/specs")
  await page.locator(".spec-tree__item", { hasText: "CI Pipeline" }).click()

  await expect(page).toHaveURL(/[?&]path=specs%2Fci%2Fpipeline\.md/)
  await expect(page.locator(".spec-view__title")).toHaveText("CI Pipeline")
  await expect(page.locator(".spec-view")).toContainText("jobs wired by needs")
  await expect(page.locator(".spec-tree__item.is-active")).toHaveText("CI Pipeline")
})

test("Specs: ↑/↓ move the selection between specs", async ({ page }) => {
  await seedToken(page)
  await mockApi(page)
  await mockSpecs(page, SPECS)

  await page.goto("/alice/demo/specs")
  await expect(page.locator(".spec-view__title")).toHaveText("SSH Transport")

  // ArrowDown advances to the next spec in tree order (the ci/ one).
  await page.keyboard.press("ArrowDown")
  await expect(page.locator(".spec-view__title")).toHaveText("CI Pipeline")
  await expect(page.locator(".spec-tree__item.is-active")).toHaveText("CI Pipeline")

  // ArrowUp goes back.
  await page.keyboard.press("ArrowUp")
  await expect(page.locator(".spec-view__title")).toHaveText("SSH Transport")
})

test("Specs: empty repo shows the convention empty state", async ({ page }) => {
  await seedToken(page)
  await mockApi(page)
  await mockSpecs(page, { ref: "main", specs: [] })

  await page.goto("/alice/demo/specs")

  await expect(page.locator(".specs-layout")).toHaveCount(0)
  await expect(page.locator(".empty")).toContainText("No specs yet")
  await expect(page.locator(".empty")).toContainText("docs/specs.md")
})

test("Specs: ⌘P quick-open fuzzy-jumps to a spec", async ({ page }) => {
  await seedToken(page)
  await mockApi(page)
  await mockSpecs(page, SPECS)

  await page.goto("/alice/demo/specs")
  await expect(page.locator(".spec-view__title")).toHaveText("SSH Transport")

  // The print shortcut opens the palette instead.
  await page.keyboard.press("Control+p")
  await expect(page.locator(".quickopen__input")).toBeFocused()

  // Fuzzy query narrows to the CI Pipeline spec; Enter opens it.
  await page.locator(".quickopen__input").fill("pipe")
  await expect(page.locator(".quickopen__item")).toHaveCount(1)
  await expect(page.locator(".quickopen__item").first()).toContainText("CI Pipeline")
  await page.keyboard.press("Enter")

  // Palette closes and the chosen spec is now rendered + reflected in ?path.
  await expect(page.locator(".quickopen__input")).toBeHidden()
  await expect(page.locator(".spec-view__title")).toHaveText("CI Pipeline")
  await expect(page).toHaveURL(/[?&]path=specs%2Fci%2Fpipeline\.md/)
})

test("Specs: the Jump button opens the palette and a click selects", async ({ page }) => {
  await seedToken(page)
  await mockApi(page)
  await mockSpecs(page, SPECS)

  await page.goto("/alice/demo/specs")
  await page.locator(".spec-jump").click()
  await expect(page.locator(".quickopen__input")).toBeVisible()

  // Empty query lists every spec; clicking one opens it.
  await expect(page.locator(".quickopen__item")).toHaveCount(2)
  await page.locator(".quickopen__item", { hasText: "CI Pipeline" }).click()
  await expect(page.locator(".quickopen__input")).toBeHidden()
  await expect(page.locator(".spec-view__title")).toHaveText("CI Pipeline")
})
