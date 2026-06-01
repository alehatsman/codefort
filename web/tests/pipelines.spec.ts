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

// A run whose single job mixes a stdout line, an stderr line (progress, not an
// error), an ANSI-colored line, and a failed step — to pin the log-coloring
// rules.
function coloringSeed(): Partial<State> {
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
        commit_msg: "ci: exercise log coloring",
        commit_author: "Alice Example",
        ref: "refs/heads/main",
        event: "push",
        trigger: "alice",
        status: "failed",
        created_at: iso,
        started_at: iso,
        finished_at: iso,
        jobs: [
          { name: "checks", status: "failed", exit_code: 1, started_at: iso, finished_at: iso },
        ],
        events: {
          checks: [
            { seq: 1, type: "run.started", time: 0, data: { total_steps: 2 } },
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
              data: { step_id: "step-0001", stream: "stdout", line: "compiling main.go" },
            },
            {
              seq: 4,
              type: "step.stderr",
              time: 0,
              data: {
                step_id: "step-0001",
                stream: "stderr",
                line: "go: downloading example.com/foo v1.2.3",
              },
            },
            {
              seq: 5,
              type: "step.completed",
              time: 0,
              data: { step_id: "step-0001", duration_ms: 5, result: { status: "ok", rc: 0 } },
            },
            {
              seq: 6,
              type: "step.started",
              time: 0,
              data: { step_id: "step-0002", action: "shell", name: "go test ./..." },
            },
            {
              seq: 7,
              type: "step.stdout",
              time: 0,
              data: {
                step_id: "step-0002",
                stream: "stdout",
                // ANSI: green "ok" then reset.
                line: "\u001b[32mok\u001b[0m  example/pkg  0.10s",
              },
            },
            {
              seq: 8,
              type: "step.completed",
              time: 0,
              data: { step_id: "step-0002", duration_ms: 9, result: { status: "failed", rc: 1 } },
            },
            { seq: 9, type: "run.completed", time: 0, data: { total_steps: 2 } },
          ],
        },
      },
    ],
  }
}

test("stderr log lines are not painted red — stderr is a stream, not an error", async ({
  page,
}) => {
  await mockApi(page, coloringSeed())
  await page.goto("/alice/demo/pipelines/1")

  const stdout = page.getByText("compiling main.go")
  const stderr = page.getByText("go: downloading example.com/foo")
  await expect(stdout).toBeVisible()
  await expect(stderr).toBeVisible()

  // The stderr line carries the stream marker class…
  await expect(stderr).toHaveClass(/ci-log__line--stderr/)
  // …but renders in the same color as stdout — no red error styling.
  const color = (loc: typeof stdout) => loc.evaluate((el) => getComputedStyle(el).color)
  expect(await color(stderr)).toBe(await color(stdout))
})

test("ANSI color escapes render as styled spans, not raw text", async ({ page }) => {
  await mockApi(page, coloringSeed())
  await page.goto("/alice/demo/pipelines/1")

  // The green "ok" is a styled span; the raw escape never reaches the DOM text.
  const green = page.locator(".ansi-fg--green")
  await expect(green).toHaveText("ok")
  await expect(page.getByText("example/pkg")).toBeVisible()
  // The raw SGR escape ("[32m") never lands in the rendered text.
  await expect(page.locator(".ci-log").last()).not.toContainText("[32m")
})

test("a failed step flags its header, leaving the log body neutral", async ({ page }) => {
  await mockApi(page, coloringSeed())
  await page.goto("/alice/demo/pipelines/1")

  const failedHead = page.locator(".ci-step__head--failed")
  await expect(failedHead).toHaveCount(1)
  await expect(failedHead).toContainText("go test ./...")
})

test("DAG job carries its status as a class so pass/fail is scannable", async ({ page }) => {
  await mockApi(page, enabledSeed())
  await page.goto("/alice/demo/pipelines/1")

  // Both jobs succeeded → both buttons carry the success status modifier, and
  // the open job is flagged active.
  await expect(page.locator(".ci-dag__job--success")).toHaveCount(2)
  await expect(page.locator(".ci-dag__job.is-active")).toHaveCount(1)
})

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

test("a mooncake-pilot run renders its steps, not a blank transcript", async ({ page }) => {
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
        issue_number: 6,
        execution_model: "mooncake-pilot",
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
            { seq: 1, type: "agent.turn.started", time: 0, data: { turn: 1, prompt: "Issue #6: do it" } },
            // Pilot events arrive under data.mooncake, not data.claude (#123).
            {
              seq: 2,
              type: "agent.message",
              time: 0,
              data: { mooncake: { type: "plan.loaded", data: { total_steps: 2 } } },
            },
            {
              seq: 3,
              type: "agent.message",
              time: 0,
              data: {
                mooncake: {
                  type: "step.started",
                  data: { step_id: "s1", action: "file.write", name: "create CHANGELOG.md" },
                },
              },
            },
            {
              seq: 4,
              type: "agent.message",
              time: 0,
              data: {
                mooncake: {
                  type: "step.completed",
                  data: { step_id: "s1", duration_ms: 1, result: { status: "changed" } },
                },
              },
            },
            {
              seq: 5,
              type: "agent.message",
              time: 0,
              data: {
                mooncake: {
                  type: "step.started",
                  data: { step_id: "s2", action: "cmd", name: "report progress" },
                },
              },
            },
            {
              seq: 6,
              type: "agent.message",
              time: 0,
              data: {
                mooncake: {
                  type: "step.stdout",
                  data: { step_id: "s2", stream: "stdout", line: "commented on #6 by agent-run-9" },
                },
              },
            },
            {
              seq: 7,
              type: "agent.message",
              time: 0,
              data: {
                mooncake: {
                  type: "step.completed",
                  // result.target carries the rendered argv for cmd/shell steps.
                  data: {
                    step_id: "s2",
                    duration_ms: 18,
                    result: { status: "changed", target: "mgit issue comment 6 --body done" },
                  },
                },
              },
            },
            {
              seq: 8,
              type: "agent.message",
              time: 0,
              data: {
                mooncake: {
                  type: "run.completed",
                  data: { success_steps: 2, changed_steps: 2, failed_steps: 0, duration_ms: 1120 },
                },
              },
            },
            // The pilot looped 3 times before settling — surfaced only because >1.
            {
              seq: 9,
              type: "agent.message",
              time: 0,
              data: {
                mooncake: {
                  type: "pilot.completed",
                  data: { status: "success", stop_reason: "success", iterations: 3 },
                },
              },
            },
            // num_turns/duration are 0 for pilot — must not render "0 steps · 0 ms".
            {
              seq: 10,
              type: "agent.turn.completed",
              time: 0,
              data: { turn: 1, status: "success", num_turns: 0, duration_ms: 0 },
            },
          ],
        },
      },
    ],
  })
  await page.goto("/alice/demo/agents/1")

  const transcript = page.locator(".agent-transcript")
  await expect(transcript).toBeVisible()
  await expect(page.getByText("🔧 file.write · create CHANGELOG.md")).toBeVisible()
  await expect(page.getByText("🔧 cmd · report progress")).toBeVisible()
  // The executed command line (result.target) is surfaced as a `$ …` line.
  await expect(page.getByText("$ mgit issue comment 6 --body done")).toBeVisible()
  // The shell step's stdout is surfaced under its step.
  await expect(page.getByText("commented on #6 by agent-run-9")).toBeVisible()
  // The real aggregate replaces the bogus "0 steps · 0 ms".
  await expect(page.getByText("2 ok · 2 changed")).toBeVisible()
  await expect(transcript).not.toContainText("0 steps")
  // The pilot loop count (otherwise invisible) is surfaced because it re-planned.
  await expect(page.getByText("3 iterations")).toBeVisible()
})

test("an in-flight agent turn shows a planning/working indicator; a parked run shows none (#133)", async ({
  page,
}) => {
  const repo = {
    id: 1,
    owner: "alice",
    name: "demo",
    created_at: iso,
    open_issues: 1,
    total_issues: 1,
    ci_enabled: true,
  }
  const runBase = {
    kind: "agent" as const,
    issue_number: 6,
    execution_model: "mooncake-pilot" as const,
    commit_sha: "deadbeefcafe1234",
    ref: "HEAD",
    event: "agent",
    trigger: "agent#17",
    created_at: iso,
    started_at: iso,
  }
  const turnStarted = {
    seq: 1,
    type: "agent.turn.started",
    time: 0,
    data: { turn: 1, prompt: "Issue #6: do it" },
  }
  const step = (seq: number, type: string, data: Record<string, unknown>) => ({
    seq,
    type: "agent.message",
    time: 0,
    data: { mooncake: { type, data } },
  })
  await mockApi(page, {
    repos: [repo],
    ciRuns: [
      // #1 running, turn started but no content yet → "Planning…"
      {
        ...runBase,
        number: 1,
        status: "running",
        finished_at: null,
        jobs: [{ name: "agent", status: "running", exit_code: null, started_at: iso, finished_at: null }],
        events: { agent: [turnStarted] },
      },
      // #2 running, a step has begun (no turn.completed) → "Working…"
      {
        ...runBase,
        number: 2,
        status: "running",
        finished_at: null,
        jobs: [{ name: "agent", status: "running", exit_code: null, started_at: iso, finished_at: null }],
        events: {
          agent: [
            turnStarted,
            step(2, "step.started", { step_id: "s1", action: "cmd", name: "do a thing" }),
            step(3, "step.completed", { step_id: "s1", duration_ms: 5, result: { status: "ok" } }),
          ],
        },
      },
      // #3 parked awaiting input — turn completed → no indicator.
      {
        ...runBase,
        number: 3,
        status: "awaiting_input",
        finished_at: null,
        jobs: [{ name: "agent", status: "running", exit_code: null, started_at: iso, finished_at: null }],
        events: {
          agent: [
            turnStarted,
            { seq: 2, type: "agent.turn.completed", time: 0, data: { turn: 1, status: "success" } },
          ],
        },
      },
    ],
  })

  // #1: planning — the busy hint shows before any content, no "Working…" yet.
  await page.goto("/alice/demo/agents/1")
  await expect(page.getByText("Planning…")).toBeVisible()
  await expect(page.getByText("Working…")).toHaveCount(0)

  // #2: a step has begun → flips to "Working…".
  await page.goto("/alice/demo/agents/2")
  await expect(page.getByText("Working…")).toBeVisible()
  await expect(page.getByText("Planning…")).toHaveCount(0)

  // #3: parked awaiting input — neither indicator, just the transcript.
  await page.goto("/alice/demo/agents/3")
  await expect(page.locator(".agent-transcript")).toBeVisible()
  await expect(page.locator(".agent-working")).toHaveCount(0)
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

  // The handoff settles server-side. Because "finishing" stays a live status,
  // the detail poll keeps running and the UI transitions to the terminal note
  // on its own — no manual reload (regression: #124).
  state.ciRuns[0].status = "success"
  state.ciRuns[0].finished_at = iso
  await expect(page.getByText(/This agent run has finished/)).toBeVisible()
})

test("a running agent run can be force-stopped while Finish is disabled (#146)", async ({ page }) => {
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
        status: "running",
        commit_sha: "deadbeefcafe1234",
        ref: "HEAD",
        event: "agent",
        trigger: "agent#17",
        created_at: iso,
        started_at: iso,
        finished_at: null,
        jobs: [{ name: "agent", status: "running", exit_code: null, started_at: iso, finished_at: null }],
        events: {
          agent: [{ seq: 1, type: "agent.turn.started", time: 0, data: { turn: 1, prompt: "Issue #5: do it" } }],
        },
      },
    ],
  })
  await page.goto("/alice/demo/pipelines/1")

  // Mid-turn: Finish is disabled (it needs a parked run), but Stop is available.
  await expect(page.getByRole("button", { name: "Finish" })).toBeDisabled()
  const stop = page.getByRole("button", { name: "Stop" })
  await expect(stop).toBeEnabled()

  await stop.click()

  // The run is canceled server-side and the box switches to the terminal note.
  await expect.poll(() => state.ciRuns[0].status).toBe("canceled")
  await expect(page.getByText(/This agent run has finished/)).toBeVisible()
})

test("run detail surfaces commit context, the job DAG, and step commands", async ({ page }) => {
  await mockApi(page, enabledSeed())
  await page.goto("/alice/demo/pipelines/1")

  // Commit context: subject + author, not just a SHA.
  await expect(page.getByText("fix: handle empty input")).toBeVisible()
  await expect(page.getByText(/Alice Example/)).toBeVisible()

  // The short SHA links through to the commit detail page.
  await expect(page.getByRole("link", { name: "deadbee" })).toHaveAttribute(
    "href",
    "/alice/demo/commit/deadbeefcafe1234"
  )

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
