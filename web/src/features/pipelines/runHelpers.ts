import type { CIRun, CIRunExecutionModel } from "@/api/types"

// kind selects which runs a view serves: "ci" is the Pipelines tab, "agent" is
// the Agents tab. The run resource and its event stream are shared; the body
// rendering (CIRunBody vs AgentRunBody) and a few list/detail labels diverge.
export type RunKind = "ci" | "agent"

// runsBasePath is the route segment a kind's runs live under.
export function runsBasePath(kind: RunKind): string {
  return kind === "agent" ? "agents" : "pipelines"
}

// executionModelLabel is the human label for an agent run's execution model
// (#110). Falls back to the default model when the field is absent (older runs).
export function executionModelLabel(model: CIRunExecutionModel | undefined): string {
  switch (model) {
    case "mooncake-agent":
      return "Mooncake agent"
    default:
      return "Claude (edit)"
  }
}

export function shortSHA(sha: string): string {
  return sha.length > 7 ? sha.slice(0, 7) : sha
}

export function shortRef(ref: string): string {
  return ref.replace(/^refs\/heads\//, "").replace(/^refs\/tags\//, "")
}

export function runDuration(run: Pick<CIRun, "started_at" | "finished_at" | "status">): string {
  if (!run.started_at) return "—"
  const start = new Date(run.started_at).getTime()
  const end = run.finished_at ? new Date(run.finished_at).getTime() : Date.now()
  return formatDuration(end - start)
}

export function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms} ms`
  const s = Math.round(ms / 1000)
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  return `${m}m ${s % 60}s`
}
