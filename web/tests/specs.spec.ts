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
    body: "## Intent\nGit over SSH as an opt-in second port.\n## Behavior\nWHEN pushed THEN verify.",
    meta: { owners: ["aleh"], covers: ["internal/ssh/**"], last_verified: "2026-06-02", alignment: 0.91 },
  },
}

// Semantic-search result for the ⌘⇧F flow (the dex-backed endpoint is mocked).
const SEARCH = {
  query: "verify",
  hits: [
    {
      path: "specs/ssh-transport.md",
      section: "Behavior",
      line: 9,
      snippet: "WHEN pushed THEN verify.",
      score: 0.92,
    },
  ],
}

// mockSearch intercepts POST .../specs/search. Registered after mockSpecs so it
// wins (Playwright matches newest-first); non-POST falls through to the GET
// content/list routes.
async function mockSearch(page: Page, result: unknown) {
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/specs\/search$/, (route) => {
    if (route.request().method() !== "POST") return route.fallback()
    return route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(result),
    })
  })
}

async function mockSpecs(page: Page, list: unknown) {
  // Content endpoint (.../specs/<path>) — registered first; its regex requires
  // a trailing segment so it's disjoint from the list route below.
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/specs\/[^?]+/, (route) => {
    if (route.request().method() !== "GET") return route.fallback()
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

// mockWrite intercepts PUT .../specs/<path>. Registered after mockSpecs so it
// wins for PUT; non-PUT falls through to the GET content/list routes.
async function mockWrite(page: Page, result: unknown) {
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/specs\/[^?]+/, (route) => {
    if (route.request().method() !== "PUT") return route.fallback()
    return route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(result),
    })
  })
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
  // The active tab now carries a spec-count pill, so match the label loosely.
  await expect(page.locator(".tab.is-active")).toContainText("Specs")

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

  // The print shortcut opens the palette instead (the path-jump one, not search).
  const input = page.locator(".quickopen:not(.specsearch):not(.cmdk) .quickopen__input")
  await page.keyboard.press("Control+p")
  await expect(input).toBeFocused()

  // Fuzzy query narrows to the CI Pipeline spec; Enter opens it.
  await input.fill("pipe")
  await expect(page.locator(".quickopen__item")).toHaveCount(1)
  await expect(page.locator(".quickopen__item").first()).toContainText("CI Pipeline")
  await page.keyboard.press("Enter")

  // Palette closes and the chosen spec is now rendered + reflected in ?path.
  await expect(input).toBeHidden()
  await expect(page.locator(".spec-view__title")).toHaveText("CI Pipeline")
  await expect(page).toHaveURL(/[?&]path=specs%2Fci%2Fpipeline\.md/)
})

test("Specs: ⌘P quick-open lists every spec and a click selects", async ({ page }) => {
  await seedToken(page)
  await mockApi(page)
  await mockSpecs(page, SPECS)

  await page.goto("/alice/demo/specs")
  await page.keyboard.press("Control+p")
  await expect(page.locator(".quickopen:not(.specsearch):not(.cmdk) .quickopen__input")).toBeVisible()

  // Empty query lists every spec; clicking one opens it.
  await expect(page.locator(".quickopen__item")).toHaveCount(2)
  await page.locator(".quickopen__item", { hasText: "CI Pipeline" }).click()
  await expect(page.locator(".quickopen:not(.specsearch):not(.cmdk) .quickopen__input")).toBeHidden()
  await expect(page.locator(".spec-view__title")).toHaveText("CI Pipeline")
})

test("Specs: ⌘⇧F semantic search lists hits and deep-links to a section", async ({ page }) => {
  await seedToken(page)
  await mockApi(page)
  await mockSpecs(page, SPECS)
  await mockSearch(page, SEARCH)

  await page.goto("/alice/demo/specs")
  await page.keyboard.press("Control+Shift+F")
  await expect(page.locator(".specsearch .quickopen__input")).toBeFocused()

  // Query → results: a hit shows its section + snippet.
  await page.locator(".specsearch .quickopen__input").fill("verify")
  await page.keyboard.press("Enter")
  const hit = page.locator(".specsearch__hit")
  await expect(hit).toHaveCount(1)
  await expect(hit).toContainText("Behavior")
  await expect(hit).toContainText("WHEN pushed THEN verify.")

  // Picking it opens the spec and deep-links to the section (?path + ?section).
  await hit.click()
  await expect(page.locator(".specsearch .quickopen__input")).toBeHidden()
  await expect(page.locator(".spec-view__title")).toHaveText("SSH Transport")
  await expect(page).toHaveURL(/[?&]path=specs%2Fssh-transport\.md/)
  await expect(page).toHaveURL(/[?&]section=Behavior/)
  // The targeted section heading is present in the rendered spec.
  await expect(page.locator(".spec-view h2", { hasText: "Behavior" })).toBeVisible()
})


test("Specs: edit a spec — live preview, save to a branch, PR link", async ({ page }) => {
  await seedToken(page)
  await mockApi(page)
  await mockSpecs(page, SPECS)
  await mockWrite(page, { branch: "spec/ssh-transport", commit: "abcdef1234567890", created: true })

  await page.goto("/alice/demo/specs")
  await expect(page.locator(".spec-view__title")).toHaveText("SSH Transport")

  // A History link points at the spec's commit history.
  await expect(page.locator(".spec-view__history")).toHaveAttribute(
    "href",
    "/alice/demo/commits/specs/ssh-transport.md"
  )

  // Enter edit mode → split editor with the raw content.
  await page.locator(".spec-view__actions").getByRole("button", { name: "Edit" }).click()
  const editor = page.locator(".spec-editor__input")
  await expect(editor).toContainText("# SSH Transport")

  // Editing updates the live preview.
  await editor.fill("# SSH Transport\n## Extra\nbrand new section.")
  await expect(page.locator(".spec-editor__preview")).toContainText("Extra")

  // Save commits to a branch and surfaces a PR link.
  await page.getByRole("button", { name: "Save to branch" }).click()
  const saved = page.locator(".spec-editor__saved")
  await expect(saved).toContainText("spec/ssh-transport")
  await expect(saved.getByRole("link", { name: /pull request/ })).toHaveAttribute(
    "href",
    "/alice/demo/compare?head=spec%2Fssh-transport"
  )

  // Done returns to the read view.
  await page.getByRole("button", { name: "Done" }).click()
  await expect(page.locator(".spec-editor")).toHaveCount(0)
  await expect(page.locator(".spec-view__title")).toHaveText("SSH Transport")
})

test("Specs: ⌘K command palette lists workflows and runs one", async ({ page }) => {
  await seedToken(page)
  await mockApi(page)
  await mockSpecs(page, SPECS)

  await page.goto("/alice/demo/specs")
  await page.keyboard.press("Control+k")
  await expect(page.locator(".cmdk .quickopen__input")).toBeFocused()

  // The registry exposes the wired commands.
  await expect(page.locator(".cmdk__item")).toContainText(["New spec", "Jump to a spec", "Search"])

  // Filtering narrows; running "Jump" hands off to the quick-open palette.
  await page.locator(".cmdk .quickopen__input").fill("jump")
  await expect(page.locator(".cmdk__item")).toHaveCount(1)
  await page.keyboard.press("Enter")
  await expect(page.locator(".cmdk .quickopen__input")).toBeHidden()
  await expect(page.locator(".quickopen:not(.specsearch):not(.cmdk) .quickopen__input")).toBeVisible()
})

test("Specs: ⌘K → New spec prompts for a name and opens a templated draft", async ({ page }) => {
  await seedToken(page)
  await mockApi(page)
  await mockSpecs(page, SPECS)

  await page.goto("/alice/demo/specs")
  await page.keyboard.press("Control+k")
  await page.locator(".cmdk__item", { hasText: "New spec" }).click()

  // The palette switches to the name prompt; the command list is gone.
  await expect(page.locator(".cmdk__item")).toHaveCount(0)
  await page.locator(".cmdk .quickopen__input").fill("data retention")
  await page.keyboard.press("Enter")

  // A templated draft opens in the editor — frontmatter + a titled H1.
  const editor = page.locator(".spec-editor__input")
  await expect(editor).toContainText("id: data-retention")
  await expect(editor).toContainText("# Data Retention")
  await expect(editor).toContainText("## Behavior")
  await expect(page.locator(".spec-editor__preview")).toContainText("Data Retention")
})

test("Specs: empty repo offers New spec via the command palette", async ({ page }) => {
  await seedToken(page)
  await mockApi(page)
  await mockSpecs(page, { ref: "main", specs: [] })

  await page.goto("/alice/demo/specs")
  await page.getByRole("button", { name: "New spec" }).click()
  await expect(page.locator(".cmdk__item", { hasText: "New spec" })).toBeVisible()
})

test("Specs: a verified spec shows the truth panel + stale badge", async ({ page }) => {
  await seedToken(page)
  await mockApi(page)
  await mockSpecs(page, SPECS)
  // Override the content route for the ssh spec to carry a verification.
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/specs\/ssh-transport\.md(\?.*)?$/, (route) => {
    if (route.request().method() !== "GET") return route.fallback()
    return route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        ref: "main",
        path: "specs/ssh-transport.md",
        id: "ssh-transport",
        title: "SSH Transport",
        status: "living",
        content: "# SSH Transport\n## Behavior\nWHEN x THEN y.",
        body: "# SSH Transport\n## Behavior\nWHEN x THEN y.",
        sections: [],
        checklist: [],
        verification: {
          alignment: 0.75,
          markers: [
            { line: 6, text: "WHEN x THEN y.", marker: "drifted", note: "server.go:10 differs" },
            { line: 4, text: "# SSH Transport", marker: "aligned" },
          ],
          conflicts: ["behavior at L6 drifted"],
          verified_at: new Date().toISOString(),
          commit: "deadbeef",
          stale: true,
        },
      }),
    })
  })

  await page.goto("/alice/demo/specs?path=specs%2Fssh-transport.md")
  await expect(page.locator(".spec-view__title")).toHaveText("SSH Transport")

  // The header truth badge shows alignment + stale.
  const badge = page.locator(".spec-truth")
  await expect(badge).toContainText("75% aligned")
  await expect(badge).toContainText("stale")

  // The verification panel lists the per-line markers (the gutter) + conflict.
  const panel = page.locator(".spec-verify")
  await expect(panel).toContainText("Last verification")
  await expect(panel.locator(".spec-marker--drifted")).toBeVisible()
  await expect(panel).toContainText("server.go:10 differs")
  await expect(panel).toContainText("behavior at L6 drifted")
})

// A running spec-verify run, returned by the verify trigger and the run detail.
function verifyRun(number: number, status: string) {
  return {
    number,
    kind: "spec-verify",
    commit_sha: "abc123",
    ref: "main",
    event: "spec-verify",
    status,
    created_at: new Date().toISOString(),
    started_at: null,
    finished_at: null,
    jobs: [],
  }
}

test("Specs: Verify triggers a run and links to the SSE run viewer", async ({ page }) => {
  await seedToken(page)
  await mockApi(page)
  await mockSpecs(page, SPECS)
  // The trigger returns a running run; the run detail keeps reporting running so
  // the watcher's link stays put for the assertion.
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/specs\/verify$/, (route) =>
    route.fulfill({ status: 202, contentType: "application/json", body: JSON.stringify(verifyRun(7, "running")) })
  )
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/runs\/7$/, (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(verifyRun(7, "running")) })
  )

  await page.goto("/alice/demo/specs")
  await page.locator(".spec-view__actions").getByRole("button", { name: "Verify" }).click()

  const link = page.locator(".spec-verify-status").getByRole("link", { name: "run #7" })
  await expect(link).toBeVisible()
  await expect(link).toHaveAttribute("href", "/alice/demo/pipelines/7")
})

test("Specs: Verify all fans out over the stale-candidate set", async ({ page }) => {
  await seedToken(page)
  await mockApi(page)
  await mockSpecs(page, SPECS)
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/specs\/drift(\?.*)?$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        ref: "main",
        specs: [
          { path: "specs/ssh-transport.md", id: "ssh-transport", status: "stale" },
          { path: "specs/ci/pipeline.md", id: "pipeline", status: "unverified" },
          { path: "specs/fresh.md", id: "fresh", status: "fresh" },
          { path: "specs/none.md", id: "none", status: "uncovered" },
        ],
      }),
    })
  )
  let verifyPosts = 0
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/specs\/verify$/, (route) => {
    verifyPosts++
    return route.fulfill({
      status: 202,
      contentType: "application/json",
      body: JSON.stringify(verifyRun(verifyPosts, "queued")),
    })
  })

  await page.goto("/alice/demo/specs")
  await page.getByRole("button", { name: "Verify all stale" }).click()

  // Only the stale + unverified specs are queued (2), not fresh/uncovered.
  await expect(page.locator(".spec-verify-all")).toContainText("Queued 2 run(s)")
  expect(verifyPosts).toBe(2)
})
