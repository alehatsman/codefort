import { test, expect } from "@playwright/test"
import { mockApi, seedToken, type State } from "./mockApi"

test.beforeEach(async ({ page }) => {
  await seedToken(page)
})

const iso = new Date().toISOString()

// One commit in the history with a modify + an add, so the diff exercises
// context/del/add lines and two files.
function seed(): Partial<State> {
  const sha = "abc1234def5678abc1234def5678abc1234def56"
  return {
    commits: [
      {
        sha,
        short_sha: "abc1234",
        subject: "tweak greeting and add helper",
        author: "alice",
        email: "a@b.c",
        date: iso,
      },
    ],
    commitDetails: {
      [sha]: {
        commit: {
          sha,
          short_sha: "abc1234",
          subject: "tweak greeting and add helper",
          body: "longer explanation\nspanning two lines",
          author: "alice",
          email: "a@b.c",
          date: iso,
        },
        parents: ["0000000feeedface0000000feeedface00000000"],
        additions: 3,
        deletions: 1,
        truncated: false,
        files: [
          {
            old_path: "main.go",
            new_path: "main.go",
            status: "modified",
            binary: false,
            additions: 1,
            deletions: 1,
            hunks: [
              {
                header: "func main() {",
                lines: [
                  { kind: "context", old: 1, new: 1, text: "func main() {" },
                  { kind: "del", old: 2, new: 0, text: '\tprintln("hi")' },
                  { kind: "add", old: 0, new: 2, text: '\tprintln("hello")' },
                  { kind: "context", old: 3, new: 3, text: "}" },
                ],
              },
            ],
          },
          {
            old_path: "",
            new_path: "helper.go",
            status: "added",
            binary: false,
            additions: 2,
            deletions: 0,
            hunks: [
              {
                header: "",
                lines: [
                  { kind: "add", old: 0, new: 1, text: "package main" },
                  { kind: "add", old: 0, new: 2, text: "func helper() {}" },
                ],
              },
            ],
          },
        ],
      },
    },
  }
}

test("open a commit from history and render its split diff", async ({ page }) => {
  await mockApi(page, seed())

  // From the commits list, click through to the commit detail.
  await page.goto("/alice/demo/commits")
  await page.getByRole("link", { name: "tweak greeting and add helper" }).click()

  // Header: subject + body + parent link.
  await expect(page.getByRole("heading", { name: "tweak greeting and add helper" })).toBeVisible()
  await expect(page.getByText("longer explanation")).toBeVisible()
  await expect(page.getByRole("link", { name: "0000000" })).toBeVisible()

  // Summary bar: file count + totals.
  await expect(page.getByText("2 files changed")).toBeVisible()
  await expect(page.getByText("+3")).toBeVisible()

  // Split is the default layout: four columns per row (two num + two code).
  const firstRow = page.locator("table.diff-split tr.diff-row").first()
  await expect(firstRow.locator("td")).toHaveCount(4)

  // The changed lines render on the correct sides.
  await expect(page.locator("td.diff-code--del").first()).toContainText('println("hi")')
  await expect(page.locator("td.diff-code--add").first()).toContainText('println("hello")')

  // Both files are present.
  await expect(page.getByText("main.go")).toBeVisible()
  await expect(page.getByText("helper.go")).toBeVisible()
})

test("toggle from split to unified diff", async ({ page }) => {
  await mockApi(page, seed())
  const sha = "abc1234def5678abc1234def5678abc1234def56"
  await page.goto(`/alice/demo/commit/${sha}`)

  // Split table present by default.
  await expect(page.locator("table.diff-split").first()).toBeVisible()

  await page.getByRole("button", { name: "Unified" }).click()

  // Now a unified table; split is gone.
  await expect(page.locator("table.diff-unified").first()).toBeVisible()
  await expect(page.locator("table.diff-split")).toHaveCount(0)
  // Unified rows carry three columns (old num, new num, code).
  await expect(page.locator("table.diff-unified tr.diff-row").first().locator("td")).toHaveCount(3)
})
