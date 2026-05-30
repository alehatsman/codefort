import { test, expect } from "@playwright/test"
import { mockApi, seedToken } from "./mockApi"

// Per-segment dex summaries on the path breadcrumb (issues #17, #43). The
// breadcrumb is the single minimal nav header: every segment dex has prose for
// — the repo, any directory, the current file — carries that summary as a
// native title tooltip and is marked with the --info affordance (dotted
// underline) so users know a hover is available. Segments dex didn't summarize
// stay plain. On the blob view the file summary fills the leaf; the repo +
// package overview makes the parent crumbs hoverable too.

const blobBody = JSON.stringify({
  ref: "main",
  path: "src/app.ts",
  size: 12,
  binary: false,
  too_large: false,
  content: "const x = 1\n",
})

test.beforeEach(async ({ page }) => {
  await seedToken(page)
})

function routeBlob(page: import("@playwright/test").Page) {
  return page.route(/\/api\/repos\/[^/]+\/[^/]+\/blob(\?.*)?$/, (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body: blobBody })
  )
}

// dex is up and this repo is indexed → the intel-gated queries fire.
function routeIndexed(page: import("@playwright/test").Page) {
  return page.route(/\/api\/repos\/[^/]+\/[^/]+\/intel$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ enabled: true, found: true }),
    })
  )
}

const INFO = /path-breadcrumb__seg--info/

test("blob view: per-segment dex summaries ride the breadcrumb as hover tooltips", async ({
  page,
}) => {
  await mockApi(page)
  await routeBlob(page)
  await routeIndexed(page)
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/intel\/file-summary(\?.*)?$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        path: "src/app.ts",
        summary: "This file is the application entry point.",
      }),
    })
  )
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/intel\/overview$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        repo_summary: "Demo repository.",
        packages: [{ path: "src", summary: "Application source." }],
      }),
    })
  )

  await page.goto("/alice/demo/blob/src/app.ts")

  const crumb = page.locator(".overview .path-breadcrumb")
  await expect(crumb).toBeVisible()

  // Current (file) segment: file summary as title + the hover affordance.
  const current = crumb.locator(".path-breadcrumb__current")
  await expect(current).toHaveText("app.ts")
  await expect(current).toHaveAttribute("title", "This file is the application entry point.")
  await expect(current).toHaveClass(INFO)

  // Parent dir and repo crumbs (links) carry their package / repo summaries too.
  const src = crumb.getByRole("link", { name: "src" })
  await expect(src).toHaveAttribute("title", "Application source.")
  await expect(src).toHaveClass(INFO)
  const repo = crumb.getByRole("link", { name: "demo" })
  await expect(repo).toHaveAttribute("title", "Demo repository.")
  await expect(repo).toHaveClass(INFO)
})

test("blob view: segments stay plain when dex has no summary", async ({ page }) => {
  await mockApi(page)
  await routeBlob(page)
  await routeIndexed(page)
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/intel\/file-summary(\?.*)?$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ path: "src/app.ts", summary: "" }),
    })
  )
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/intel\/overview$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ packages: [] }),
    })
  )

  await page.goto("/alice/demo/blob/src/app.ts")

  // Plain breadcrumb: no segment carries the hover affordance.
  const crumb = page.locator(".overview .path-breadcrumb")
  await expect(crumb).toBeVisible()
  await expect(crumb.locator(".path-breadcrumb__seg--info")).toHaveCount(0)
})
