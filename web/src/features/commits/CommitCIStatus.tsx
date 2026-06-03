import { Link } from "react-router-dom"
import type { CIRun } from "@/api/types"
import CIStatusBadge from "@/features/pipelines/CIStatusBadge"
import { Tooltip } from "@/ui"

/**
 * The CI status of one commit, rendered as a badge linking to its run — shown
 * beside commits in the history list and the last-commit bars. Renders nothing
 * when the commit has no run (CI off, or the commit predates/skipped CI), so
 * callers can drop it in unconditionally.
 */
export default function CommitCIStatus({
  owner,
  repo,
  run,
}: {
  owner: string
  repo: string
  run: CIRun | undefined
}) {
  if (!run) return null
  return (
    <Tooltip label={`CI run #${run.number}: ${run.status}`}>
      <Link to={`/${owner}/${repo}/pipelines/${run.number}`} className="commit-ci">
        <CIStatusBadge status={run.status} />
      </Link>
    </Tooltip>
  )
}
