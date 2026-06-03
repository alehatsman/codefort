import type { CIJobStatus, CIRunStatus } from "@/api/types"
import { StatusPill } from "@/ui"

// CIStatusBadge maps a run or job status onto the shared StatusPill. Colors are
// driven by the `ci-badge--<status>` modifier (passed through className) so they
// track the active theme and keep their existing CSS; the status strings are
// shared between runs and jobs (jobs add "skipped"). `dense`/`capitalize` carry
// the compact CI look the issue-state Badge doesn't use.
export default function CIStatusBadge({ status }: { status: CIRunStatus | CIJobStatus }) {
  return (
    <StatusPill dense capitalize className={`ci-badge--${status}`}>
      {status.replace(/_/g, " ")}
    </StatusPill>
  )
}
