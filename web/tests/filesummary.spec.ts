import { test, expect } from "@playwright/test"
import { mockApi, seedToken } from "./mockApi"

// Per-file dex summary on the blob view (issue #17). The summary rides in
// the same collapsible OverviewCard the tree view uses: when present, the
// path breadcrumb becomes the card header and the prose is the body; when
// absent (file not summarized, or dex not indexed) it falls back to a plain
// breadcrumb with no card.

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

test("blob view: dex file summary renders in the overview card", async ({ page }) => {
  await mockApi(page)
  await routeBlob(page)
  // dex is up and this repo is indexed → the file-summary query fires.
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/intel$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ enabled: true, found: true }),
    })
  )
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

  await page.goto("/alice/demo/blob/src/app.ts")

  const card = page.locator(".overview-card")
  await expect(card).toBeVisible()
  await expect(card.locator(".overview-card__prose")).toHaveText(
    "This file is the application entry point."
  )
  // The breadcrumb is the card header, not a separate block.
  await expect(card.locator(".overview-card__head")).toBeVisible()
})

test("blob view: no card when dex has no summary for the file", async ({ page }) => {
  await mockApi(page)
  await routeBlob(page)
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/intel$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ enabled: true, found: true }),
    })
  )
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/intel\/file-summary(\?.*)?$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ path: "src/app.ts", summary: "" }),
    })
  )

  await page.goto("/alice/demo/blob/src/app.ts")

  // Plain breadcrumb, no collapsible card.
  await expect(page.locator(".blob .path-breadcrumb")).toBeVisible()
  await expect(page.locator(".overview-card")).toHaveCount(0)
})
