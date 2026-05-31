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
        commit_msg: "fix: handle empty input",
        commit_author: "Alice Example",
        ref: "refs/heads/main",
        event: "push",
        trigger: "alice",
        status: "success",
        created_at: iso,
        started_at: iso,
        finished_at: iso,
        jobs: [
          { name: "build", status: "success", exit_code: 0, started_at: iso, finished_at: iso },
          {
            name: "test",
            needs: ["build"],
            status: "success",
            exit_code: 0,
            started_at: iso,
            finished_at: iso,
          },
        ],
        events: {
          build: [
            { seq: 1, type: "run.started", time: 0, data: { total_steps: 1 } },
            {
              seq: 2,
              type: "step.started",
              time: 0,
              data: { step_id: "step-0001", action: "shell", name: "go build ./..." },
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

  // List row: the commit subject reads as the run's identity, not a bare SHA.
  await expect(page.getByText("fix: handle empty input").first()).toBeVisible()

  // Open the run; the job's streamed log line shows.
  await page.getByRole("link", { name: "#1" }).click()
  await expect(page).toHaveURL(/\/alice\/demo\/pipelines\/1$/)
  await expect(page.getByRole("heading", { name: /Run #1/ })).toBeVisible()
  await expect(page.getByText("build", { exact: true })).toBeVisible()
  await expect(page.getByText("hello from ci")).toBeVisible()
})

test("agent run renders the claude transcript instead of the job DAG", async ({ page }) => {
  await mockApi(page, {
    repos: [
      {
        id: 1,
        owner: "alice",
        name: "demo",
        created_at: iso,
        open_issues: 1,
        total_issues: 1,
        ci_enabled: true,
      },
    ],
    ciRuns: [
      {
        number: 1,
        kind: "agent",
        issue_number: 5,
        commit_sha: "deadbeefcafe1234",
        ref: "HEAD",
        event: "agent",
        trigger: "agent#17",
        status: "success",
        created_at: iso,
        started_at: iso,
        finished_at: iso,
        jobs: [{ name: "agent", status: "success", exit_code: 0, started_at: iso, finished_at: iso }],
        events: {
          agent: [
            { seq: 1, type: "run.started", time: 0, data: { total_steps: 1 } },
            { seq: 2, type: "agent.turn.started", time: 0, data: { turn: 1, prompt: "Issue #5: do it" } },
            {
              seq: 3,
              type: "agent.message",
              time: 0,
              data: {
                claude: {
                  type: "assistant",
                  message: { role: "assistant", content: [{ type: "text", text: "on it now" }] },
                },
              },
            },
            {
              seq: 4,
              type: "agent.message",
              time: 0,
              data: {
                claude: {
                  type: "assistant",
                  message: {
                    role: "assistant",
                    content: [{ type: "tool_use", name: "Bash", input: { command: "ls" } }],
                  },
                },
              },
            },
            {
              seq: 5,
              type: "agent.turn.completed",
              time: 0,
              data: { turn: 1, status: "success", num_turns: 2, duration_ms: 1200 },
            },
            { seq: 6, type: "run.completed", time: 0, data: { total_steps: 1 } },
          ],
        },
      },
    ],
  })
  await page.goto("/alice/demo/pipelines/1")

  await expect(page.getByRole("heading", { name: /Run #1/ })).toBeVisible()
  // Turn header, assistant text, and the tool call all render in the transcript.
  await expect(page.getByText("Turn 1")).toBeVisible()
  await expect(page.getByText("on it now")).toBeVisible()
  await expect(page.getByText("🔧 Bash")).toBeVisible()
  await expect(page.getByText(/Turn complete/)).toBeVisible()
  // It's the transcript, not the CI job DAG.
  await expect(page.locator(".agent-transcript")).toBeVisible()
  await expect(page.locator(".ci-dag")).toHaveCount(0)
})

test("an awaiting-input agent run shows a message box and queues a follow-up", async ({ page }) => {
  const state = await mockApi(page, {
    repos: [
      {
        id: 1,
        owner: "alice",
        name: "demo",
        created_at: iso,
        open_issues: 1,
        total_issues: 1,
        ci_enabled: true,
      },
    ],
    ciRuns: [
      {
        number: 1,
        kind: "agent",
        issue_number: 5,
        status: "awaiting_input",
        commit_sha: "deadbeefcafe1234",
        ref: "HEAD",
        event: "agent",
        trigger: "agent#17",
        created_at: iso,
        started_at: iso,
        finished_at: null,
        jobs: [{ name: "agent", status: "running", exit_code: null, started_at: iso, finished_at: null }],
        events: {
          agent: [
            { seq: 1, type: "agent.turn.started", time: 0, data: { turn: 1, prompt: "Issue #5: do it" } },
            { seq: 2, type: "agent.turn.completed", time: 0, data: { turn: 1, status: "success" } },
          ],
        },
      },
    ],
  })
  await page.goto("/alice/demo/pipelines/1")

  // The status reads "awaiting input" and the message box is available.
  await expect(page.getByText("awaiting input").first()).toBeVisible()
  const box = page.getByLabel("Message to the agent")
  await expect(box).toBeVisible()

  await box.fill("please also add a test")
  await page.getByRole("button", { name: "Send" }).click()

  // The mock recorded the queued turn.
  await expect.poll(() => state.ciRuns[0].turns?.length ?? 0).toBe(1)
  expect(state.ciRuns[0].turns?.[0].body).toBe("please also add a test")

  // Finishing the run hands off; the box switches to the finishing note.
  await page.getByRole("button", { name: "Finish" }).click()
  await expect.poll(() => state.ciRuns[0].status).toBe("finishing")
  await expect(page.getByText(/Finishing/)).toBeVisible()
})

test("run detail surfaces commit context, the job DAG, and step commands", async ({ page }) => {
  await mockApi(page, enabledSeed())
  await page.goto("/alice/demo/pipelines/1")

  // Commit context: subject + author, not just a SHA.
  await expect(page.getByText("fix: handle empty input")).toBeVisible()
  await expect(page.getByText(/Alice Example/)).toBeVisible()

  // DAG: the dependent job shows what it needs.
  await expect(page.getByText("test")).toBeVisible()
  await expect(page.getByText(/build/).last()).toBeVisible()

  // Step legibility: the log labels the step with its command, not "shell".
  await expect(page.getByText("go build ./...")).toBeVisible()
})

test("re-run enqueues a fresh run and navigates to it", async ({ page }) => {
  await mockApi(page, enabledSeed())
  await page.goto("/alice/demo/pipelines/1")

  await page.getByRole("button", { name: "Re-run" }).click()
  await expect(page).toHaveURL(/\/alice\/demo\/pipelines\/2$/)
  await expect(page.getByRole("heading", { name: /Run #2/ })).toBeVisible()
  await expect(page.getByText("queued").first()).toBeVisible()
})

test("Run pipeline triggers a manual run for the default branch", async ({ page }) => {
  await mockApi(page, enabledSeed())
  await page.goto("/alice/demo/pipelines")

  // The ref input pre-fills with the repo's default branch.
  await expect(page.getByLabel("Ref to run")).toHaveValue("main")

  await page.getByRole("button", { name: "Run pipeline" }).click()

  // The freshly queued run lands in the list.
  await expect(page.getByRole("link", { name: "#2" })).toBeVisible()
  await expect(page.getByText("queued").first()).toBeVisible()
})

test("an unresolvable ref surfaces the server error inline", async ({ page }) => {
  await mockApi(page, enabledSeed())
  await page.goto("/alice/demo/pipelines")

  await page.getByLabel("Ref to run").fill("no-such-ref")
  await page.getByRole("button", { name: "Run pipeline" }).click()

  await expect(page.getByText(/cannot resolve ref/)).toBeVisible()
  // No new run row appeared.
  await expect(page.getByRole("link", { name: "#2" })).toHaveCount(0)
})

test("Pipelines tab is reachable from the repo nav", async ({ page }) => {
  await mockApi(page, enabledSeed())
  await page.goto("/alice/demo")

  await page.getByRole("link", { name: "Pipelines" }).click()
  await expect(page).toHaveURL(/\/alice\/demo\/pipelines$/)
})

test("Agents tab lists only agent runs; Pipelines excludes them", async ({ page }) => {
  await mockApi(page, {
    repos: [
      {
        id: 1,
        owner: "alice",
        name: "demo",
        created_at: iso,
        open_issues: 1,
        total_issues: 1,
        ci_enabled: true,
      },
    ],
    ciRuns: [
      {
        number: 2,
        kind: "agent",
        issue_number: 5,
        status: "awaiting_input",
        commit_sha: "aaaa1111",
        ref: "HEAD",
        event: "agent",
        trigger: "agent#1",
        created_at: iso,
        started_at: iso,
        finished_at: null,
        jobs: [],
      },
      {
        number: 1,
        kind: "ci",
        status: "success",
        commit_sha: "bbbb2222",
        commit_msg: "ci run",
        ref: "refs/heads/main",
        event: "push",
        trigger: "alice",
        created_at: iso,
        started_at: iso,
        finished_at: iso,
        jobs: [],
      },
    ],
  })

  // Agents tab: only the agent run (#2), reachable from the nav.
  await page.goto("/alice/demo/agents")
  await expect(page.getByRole("heading", { name: "Agents" })).toBeVisible()
  await expect(page.getByText(/spawned from an issue/)).toBeVisible()
  await expect(page.getByRole("link", { name: "#2" })).toBeVisible()
  await expect(page.getByRole("link", { name: "#1" })).toHaveCount(0)
  // No "Run pipeline" form on the Agents tab.
  await expect(page.getByLabel("Ref to run")).toHaveCount(0)

  // Pipelines tab: only the CI run (#1).
  await page.getByRole("link", { name: "Pipelines" }).click()
  await expect(page.getByRole("link", { name: "#1" })).toBeVisible()
  await expect(page.getByRole("link", { name: "#2" })).toHaveCount(0)
})
