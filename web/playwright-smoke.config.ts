import { defineConfig, devices } from "@playwright/test"

const base = process.env.BASE_URL ?? "http://127.0.0.1:8080"

export default defineConfig({
  testDir: "./tests",
  testMatch: "**/phase2-smoke.spec.ts",
  fullyParallel: false,
  retries: 0,
  reporter: "list",
  use: { baseURL: base, trace: "retain-on-failure", screenshot: "only-on-failure" },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  // No webServer — tests hit the real codefortd directly.
})
