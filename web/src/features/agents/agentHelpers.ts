import type { CIRun } from "@/api/types"

// runTrigger describes what spawned an agent run, for the Agents grid's Trigger
// column. Agent runs carry the issue they were spawned from (issue_number); a
// review-profile run (tool_profile="review", the read-only review agent, #184)
// is labeled "review" rather than "issue". When there's no linked issue we fall
// back to the raw trigger identity (or "—"). `issue`, when set, is the issue
// number the cell should link to.
export function runTrigger(run: CIRun): { label: string; issue?: number } {
  if (run.issue_number) {
    const kind = run.tool_profile === "review" ? "review" : "issue"
    return { label: `${kind} #${run.issue_number}`, issue: run.issue_number }
  }
  return { label: run.trigger || "—" }
}
