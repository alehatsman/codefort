import type { CIRunStatus } from "@/api/types"

// RunChip maps a display chip (one toggle) to the backend status values it covers.
// The key is a CIRunStatus used as the chip's identity in activeStates; statuses
// is the expanded list sent as ?state= when that chip is active.
export interface RunChip {
  key: CIRunStatus
  label: string
  statuses: readonly CIRunStatus[]
}

// AGENT_CHIPS — status chips for agent run views (GlobalAgentsPage, AgentsPage).
//
// Lifecycle order: live states first, then terminal.
// Grouping rationale:
//   • "failed" absorbs "error" (infra failure) and "interrupted" (runner restart) —
//     all three mean "something went wrong; open the logs". Users never need to
//     filter these separately.
//   • "finishing" is omitted — it's a sub-second handoff state, never a useful filter.
//   • "stalled" (hit iteration cap, no progress) is kept separate from "canceled"
//     (user-initiated stop) — different root causes, different actions.
export const AGENT_CHIPS: readonly RunChip[] = [
  { key: "queued", label: "queued", statuses: ["queued"] },
  { key: "running", label: "running", statuses: ["running"] },
  { key: "awaiting_input", label: "awaiting input", statuses: ["awaiting_input"] },
  { key: "success", label: "success", statuses: ["success"] },
  { key: "failed", label: "failed", statuses: ["failed", "error", "interrupted"] },
  { key: "canceled", label: "canceled", statuses: ["canceled"] },
  { key: "stalled", label: "stalled", statuses: ["stalled"] },
]

// CI_RUN_CHIPS — status chips for CI pipeline run views (GlobalRunsPage, RunsPage).
//
// Agent-only statuses (awaiting_input, finishing, stalled) are excluded.
// "failed" absorbs "error" and "interrupted" for the same reason as above.
export const CI_RUN_CHIPS: readonly RunChip[] = [
  { key: "queued", label: "queued", statuses: ["queued"] },
  { key: "running", label: "running", statuses: ["running"] },
  { key: "success", label: "success", statuses: ["success"] },
  { key: "failed", label: "failed", statuses: ["failed", "error", "interrupted"] },
  { key: "canceled", label: "canceled", statuses: ["canceled"] },
]
