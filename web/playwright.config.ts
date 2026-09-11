import { defineConfig, devices } from "@playwright/test"

// Boots Vite dev server before running tests, points the browser at it.
// All API calls are intercepted by tests/mockApi.ts via page.route — no
// real moongitd needed for E2E.
//
// PW_PORT: override the dev-server port so concurrent agents/worktrees don't
// collide on 5173. --strictPort makes Vite fail loudly (not silently drift to
// 5174/5175/…) when the chosen port is already taken — converts phantom test
// failures from a foreign server into a clear startup error.
const port = Number(process.env.PW_PORT ?? 5173)
const baseURL = `http://localhost:${port}`

export default defineConfig({
  testDir: "./tests",
  // phase2-smoke.spec.ts needs a real moongitd on :8080 — it deliberately does
  // not use mockApi, so it can never pass here and left the suite permanently
  // 5-red. It has its own runner: playwright-smoke.config.ts (npm run test:smoke).
  testIgnore: "**/phase2-smoke.spec.ts",
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: process.env.CI ? "html" : "list",
  use: {
    baseURL,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: {
    command: `npm run dev -- --strictPort --port ${port}`,
    url: baseURL,
    reuseExistingServer: !process.env.CI,
    stdout: "ignore",
    stderr: "pipe",
  },
})
