import { expect, test } from "@playwright/test"
import { mockApi, seedToken } from "./mockApi"

// A relative link in a rendered README (e.g. `examples/`) can aim a /blob/ URL
// at a directory; the backend 400s with "path is a directory". The blob view
// must redirect to the tree view instead of printing the raw error.

test.beforeEach(async ({ page }) => {
  await seedToken(page)
})

test("a /blob/ URL pointing at a directory redirects to the tree view", async ({ page }) => {
  await mockApi(page)

  // blob?path=examples is a directory -> 400; any other blob is a real file.
  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/blob(\?.*)?$/, (route) => {
    const path = new URL(route.request().url()).searchParams.get("path")
    if (path === "examples") {
      return route.fulfill({
        status: 400,
        contentType: "application/json",
        body: JSON.stringify({ error: "path is a directory: examples" }),
      })
    }
    return route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        ref: "main",
        path,
        size: 5,
        binary: false,
        too_large: false,
        content: "# Hi\n",
      }),
    })
  })

  await page.route(/\/api\/repos\/[^/]+\/[^/]+\/tree(\?.*)?$/, (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        ref: "main",
        path: "examples",
        entries: [
          { name: "hello-world", path: "examples/hello-world", type: "tree" },
          { name: "README.md", path: "examples/README.md", type: "blob", size: 5 },
        ],
      }),
    })
  )

  await page.goto("/alice/demo/blob/examples")

  await expect(page).toHaveURL(/\/alice\/demo\/tree\/examples$/)
  await expect(page.locator(".file-tree__row", { hasText: "hello-world" })).toBeVisible()
})
