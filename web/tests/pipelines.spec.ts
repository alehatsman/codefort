import { test, expect } from "@playwright/test"
import { mockApi, seedToken, type State } from "./mockApi"

test.beforeEach(async ({ page }) => {
  await seedToken(page)
})

const iso = new Date().toISOString()

// A repo with CI on and one finished run whose single job has a log stream.
function enabledSeed(): Partial<State> {
  return {
    repos: [
      {
        id: 1,
        owner: "alice",
        name: "demo",
        created_at: iso,
        open_issues: 0,
        total_issues: 0,
        ci_enabled: true,
      },
    ],
    ciRuns: [
      {
        number: 1,
        commit_sha: "deadbeefcafe1234",
        ref: "refs/heads/main",
        event: "push",
        trigger: "alice",
        status: "success",
        created_at: iso,
        started_at: iso,
        finished_at: iso,
        jobs: [
          { name: "build", status: "success", exit_code: 0, started_at: iso, finished_at: iso },
        ],
        events: {
          build: [
            { seq: 1, type: "run.started", time: 0, data: { total_steps: 1 } },
            {
              seq: 2,
              type: "step.started",
              time: 0,
              data: { step_id: "step-0001", action: "shell" },
            },
            {
              seq: 3,
              type: "step.stdout",
              time: 0,
              data: {
                step_id: "step-0001",
                stream: "stdout",
                line: "hello from ci",
                line_number: 1,
              },
            },
            {
              seq: 4,
              type: "step.completed",
              time: 0,
              data: { step_id: "step-0001", duration_ms: 5, result: { status: "ok", rc: 0 } },
            },
            { seq: 5, type: "run.completed", time: 0, data: { total_steps: 1 } },
          ],
        },
      },
    ],
  }
}

test("disabled repo shows the enable prompt, and enabling reveals the runs list", async ({
  page,
}) => {
  await mockApi(page) // default repo: ci_enabled false, no runs
  await page.goto("/alice/demo/pipelines")

  await expect(page.getByText("CI is disabled for this repository.")).toBeVisible()
  await page.getByRole("button", { name: "Enable CI" }).click()

  // After the PATCH + repo refetch, the empty runs list renders.
  await expect(page.getByText(/No runs yet/)).toBeVisible()
})

test("pipelines tab lists runs and opens a run's job log", async ({ page }) => {
  await mockApi(page, enabledSeed())
  await page.goto("/alice/demo/pipelines")

  // List row: run number + status badge.
  await expect(page.getByRole("link", { name: "#1" })).toBeVisible()
  await expect(page.getByText("success").first()).toBeVisible()

  // Open the run; the job's streamed log line shows.
  await page.getByRole("link", { name: "#1" }).click()
  await expect(page).toHaveURL(/\/alice\/demo\/pipelines\/1$/)
  await expect(page.getByRole("heading", { name: /Run #1/ })).toBeVisible()
  await expect(page.getByText("build")).toBeVisible()
  await expect(page.getByText("hello from ci")).toBeVisible()
})

test("re-run enqueues a fresh run and navigates to it", async ({ page }) => {
  await mockApi(page, enabledSeed())
  await page.goto("/alice/demo/pipelines/1")

  await page.getByRole("button", { name: "Re-run" }).click()
  await expect(page).toHaveURL(/\/alice\/demo\/pipelines\/2$/)
  await expect(page.getByRole("heading", { name: /Run #2/ })).toBeVisible()
  await expect(page.getByText("queued").first()).toBeVisible()
})

test("Pipelines tab is reachable from the repo nav", async ({ page }) => {
  await mockApi(page, enabledSeed())
  await page.goto("/alice/demo")

  await page.getByRole("link", { name: "Pipelines" }).click()
  await expect(page).toHaveURL(/\/alice\/demo\/pipelines$/)
})
