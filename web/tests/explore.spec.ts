import { expect, test } from "@playwright/test"
import { mockApi, seedToken } from "./mockApi"

// The Explore tab merges the former Research + Summaries tabs: a hero repo
// summary, an ask box, a layered package map, and a "where work is happening"
// hotspots panel. These specs mock dex's intel endpoints (the real ones need a
// running dex daemon) and tree-commits (drives hotspots / package recency).

const PROJECT = {
  root: "/srv/demo",
  chunks: 200,
  files: 42,
  dim: 2560,
  embed_model: "qwen3-embedding:4b",
  last_indexed: new Date().toISOString(),
  pending_summaries: 5,
}

const OVERVIEW = {
  repo_summary: "Self-hosted git host and issue tracker, minimal and local-first.",
  packages: [
    { path: "internal/server", summary: "HTTP server: serves /api, git smart-HTTP, the SPA." },
    { path: "internal/storage", summary: "SQLite-backed issues, runs, and tokens." },
    { path: "web/src", summary: "React SPA — the web UI." },
  ],
}

function commit(date: string, subject: string) {
  return {
    sha: "a".repeat(40),
    short_sha: "aaaaaaa",
    subject,
    author: "alice",
    email: "alice@example.com",
    date,
  }
}

// tree-commits is fetched once per distinct package-parent dir; the page merges
// every entry map, so one map covering all package paths serves every call.
const TREE_ENTRIES = {
  "internal/server": commit("2026-05-31T12:00:00Z", "tighten server routing"),
  "internal/storage": commit("2026-05-20T12:00:00Z", "add token table"),
  "web/src": commit("2026-05-10T12:00:00Z", "initial SPA"),
}

async function mockIndexed(page: import("@playwright/test").Page) {
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/intel$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ enabled: true, found: true, project: PROJECT }),
    })
  )
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/intel\/overview$/, (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(OVERVIEW) })
  )
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/intel\/summaries$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ summaries: {} }),
    })
  )
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/tree-commits(\?.*)?$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ ref: "main", total: 3, latest: null, entries: TREE_ENTRIES }),
    })
  )
}

test("Explore: hero summary, layered package map, and hotspots render", async ({ page }) => {
  await seedToken(page)
  await mockApi(page)
  await mockIndexed(page)

  await page.goto("/alice/demo/explore")

  // Hero leads with the repo summary and headline index stats.
  await expect(page.locator(".explore-hero__summary")).toContainText("Self-hosted git host")
  await expect(page.locator(".explore-stat").filter({ hasText: "42 files" })).toBeVisible()
  await expect(page.locator(".explore-stat--pending")).toContainText("5 pending")

  // Hero is a collapsible card (open by default); clicking the name closes it,
  // hiding the summary prose while the repo name stays visible.
  await expect(page.locator(".explore-hero__summary")).toBeVisible()
  await page.locator(".explore-hero__head").click()
  await expect(page.locator(".explore-hero__summary")).toBeHidden()
  await expect(page.locator(".explore-hero__name")).toBeVisible()

  // Package map is grouped into layers, not a flat list.
  await expect(page.locator(".pkg-layer__name").filter({ hasText: "HTTP / API" })).toBeVisible()
  await expect(page.locator(".pkg-layer__name").filter({ hasText: "Storage" })).toBeVisible()
  await expect(page.locator(".pkg-layer__name").filter({ hasText: "Web UI" })).toBeVisible()
  await expect(page.locator(".pkg-card__path").filter({ hasText: "internal/server" })).toBeVisible()

  // Hotspots panel ranks the most-recently-touched package first.
  const hotspots = page.locator(".explore-section", { hasText: "Where work is happening" })
  await expect(hotspots).toBeVisible()
  await expect(hotspots.locator(".hotspot__path").first()).toHaveText("internal/server")
})

test("Explore: ask box defaults to Ask; Advanced reveals the mode picker", async ({ page }) => {
  await seedToken(page)
  await mockApi(page)
  await mockIndexed(page)
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/intel\/search$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        status: "ok",
        hits: [],
        answer: "Routing is wired up in internal/server/router.go via http.ServeMux.",
        answer_model: "qwen2.5-coder:14b",
        next_action: "Read internal/server/router.go to see route wiring.",
        suggested_reads: [],
      }),
    })
  )

  await page.goto("/alice/demo/explore")

  // Mode picker is hidden until Advanced is toggled.
  await expect(page.locator(".explore-ask__kind")).toHaveCount(0)
  await page.getByRole("button", { name: /Advanced search modes/ }).click()
  await expect(page.locator(".explore-ask__kind")).toBeVisible()

  // Asking a question leads with dex's synthesized answer (+ model
  // attribution) and still surfaces the next-action block below it.
  await page.locator(".explore-ask__input").fill("how does routing work?")
  await page.getByRole("button", { name: "Ask" }).click()
  await expect(page.locator(".ask__answer-body")).toContainText("http.ServeMux")
  await expect(page.locator(".ask__answer-model")).toContainText("qwen2.5-coder:14b")
  await expect(page.locator(".ask__next-action")).toContainText("router.go")
})

test("Explore: legacy /research and /summaries URLs redirect here", async ({ page }) => {
  await seedToken(page)
  await mockApi(page)
  await mockIndexed(page)

  await page.goto("/alice/demo/research")
  await expect(page).toHaveURL(/\/alice\/demo\/explore$/)

  await page.goto("/alice/demo/summaries")
  await expect(page).toHaveURL(/\/alice\/demo\/explore$/)

  // The Explore tab is the active one after the redirect.
  await expect(page.locator(".tab.is-active")).toHaveText("Explore")
})

test("Explore: not-indexed repo shows the dex empty state", async ({ page }) => {
  await seedToken(page)
  await mockApi(page)
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/intel$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ enabled: true, found: false }),
    })
  )

  await page.goto("/alice/demo/explore")
  await expect(page.locator(".empty")).toContainText("not indexed by dex")
  await expect(page.locator(".explore-hero")).toHaveCount(0)
})
