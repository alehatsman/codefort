import type { CIRun, CIRunExecutionModel, CIRunKind, CIRunStatus } from "@/api/types"

// isRunActive is true while a run is still pre-terminal — queued (not yet
// claimed) or running. Used to gate the Stop button on the run-detail view.
export function isRunActive(status: CIRunStatus): boolean {
  return status === "queued" || status === "running"
}

// kind selects which runs a *view* (tab) serves: "ci" is the Pipelines tab,
// "agent" is the Agents tab. This is the tab dimension, distinct from a run's
// own kind (CIRunKind, which also has "spec-verify").
export type RunKind = "ci" | "agent"

// runsBasePath is the route segment a tab's runs live under.
export function runsBasePath(kind: RunKind): string {
  return kind === "agent" ? "agents" : "pipelines"
}

// isAgentRun is the single web-side classifier for the agent family — the
// counterpart to storage.RunKind.IsAgent(). Drives the run viewer's body +
// header so it follows the run's *own* kind, not the route it was opened by
// (#270). Add a new agent kind here once, not at every call site.
export function isAgentRun(kind: CIRunKind): boolean {
  return kind === "agent" || kind === "spec-verify"
}

// executionModelLabel is the human label for an agent run's execution model
// (#110). claude-edit is the only strategy now; the mooncake-agent
// alternative was removed and old runs are backfilled to claude-edit, so this
// always resolves to the same label — kept as a function (rather than a
// literal) so call sites don't need to change if that ever stops being true.
export function executionModelLabel(_model: CIRunExecutionModel | undefined): string {
  return "Claude (edit)"
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
