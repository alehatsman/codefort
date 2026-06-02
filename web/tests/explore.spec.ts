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
  // No package graph by default → the map uses the path-name fallback. A later
  // page.route for this pattern (the graph test below) overrides this.
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/intel\/package-graph$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ status: "no-graph", nodes: [], edges: [] }),
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

// dex's real package import DAG: cmd/demo → internal/server → internal/storage.
// Import paths share the module prefix github.com/acme/demo, so the UI maps
// each to its repo-relative dir to join summaries and link into the tree.
const PACKAGE_GRAPH = {
  status: "ok",
  nodes: [
    { package: "github.com/acme/demo/cmd/demo", in_degree: 0, out_degree: 1, page_rank: 0.02 },
    {
      package: "github.com/acme/demo/internal/server",
      in_degree: 1,
      out_degree: 1,
      page_rank: 0.03,
    },
    {
      package: "github.com/acme/demo/internal/storage",
      in_degree: 1,
      out_degree: 0,
      page_rank: 0.05,
    },
    // An isolated node — a non-Go dir dex graphed with no package edges. It
    // must be hidden from the map (no structural signal) and must not drag the
    // module-prefix derivation off the Go packages onto "".
    { package: "web/src/App", in_degree: 0, out_degree: 0, page_rank: 0 },
  ],
  edges: [
    { from_package: "github.com/acme/demo/cmd/demo", to_package: "github.com/acme/demo/internal/server" },
    {
      from_package: "github.com/acme/demo/internal/server",
      to_package: "github.com/acme/demo/internal/storage",
    },
  ],
}

test("Explore: package map layers by dex import graph with degree + cross-links", async ({
  page,
}) => {
  await seedToken(page)
  await mockApi(page)
  await mockIndexed(page)
  // Override the default no-graph stub with a real import DAG (last route wins).
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/intel\/package-graph$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(PACKAGE_GRAPH),
    })
  )

  await page.goto("/alice/demo/explore")

  // Topological layers replace the path-name buckets: entry points on top,
  // foundation at the bottom — and the regex labels are gone.
  await expect(page.locator(".pkg-layer__name").filter({ hasText: "Entry points" })).toBeVisible()
  await expect(page.locator(".pkg-layer__name").filter({ hasText: "Foundation" })).toBeVisible()
  await expect(page.locator(".pkg-layer__name").filter({ hasText: "HTTP / API" })).toHaveCount(0)

  // The foundation card is internal/storage (in-degree 1, out-degree 0); its
  // degree badge reflects the import counts. Scope by the card's own path so
  // cards that merely cross-link to internal/storage don't match.
  const storage = page.locator(".pkg-card", {
    has: page.locator(".pkg-card__path", { hasText: "internal/storage" }),
  })
  await expect(storage.locator(".pkg-card__degree")).toHaveText("←1 →0")

  // internal/server cross-links to the package it uses — a real navigable link
  // into that package's tree, not a flat list. Expand the card (details) so
  // the dep rows are revealed.
  const server = page.locator(".pkg-card", {
    has: page.locator(".pkg-card__path", { hasText: "internal/server" }),
  })
  await server.locator(".pkg-card__summary").click()
  const usesLink = server
    .locator(".pkg-card__deprow", { hasText: "uses" })
    .getByRole("link", { name: "internal/storage" })
  await expect(usesLink).toHaveAttribute("href", "/alice/demo/tree/internal/storage")

  // The summary from the overview join still rides on the card.
  await expect(server.locator(".pkg-card__preview")).toContainText("HTTP server")

  // Isolated nodes (web/src/App) are hidden, the heading counts only the 3
  // linked packages and notes the hidden one, and the module prefix — derived
  // from the linked Go packages — is stripped to clean repo-relative labels.
  await expect(page.locator(".pkg-card__path", { hasText: "web/src/App" })).toHaveCount(0)
  const mapHeading = page.locator(".explore-section__heading", { hasText: "Map of the codebase" })
  await expect(mapHeading).toContainText("3 packages")
  await expect(mapHeading).toContainText("1 unlinked hidden")
  await expect(server.locator(".pkg-card__path")).toHaveText("internal/server")
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
