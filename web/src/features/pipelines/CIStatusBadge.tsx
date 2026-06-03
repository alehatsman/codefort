import clsx from "clsx"
import type { CIJobStatus, CIRunStatus } from "@/api/types"

// CIStatusBadge renders a run or job status as a colored pill. Colors are
// driven by CSS (`ci-badge--<status>`) so they track the active theme; the
// status strings are shared between runs and jobs (jobs add "skipped").
// No conditional here — clsx is used for the shared BEM-modifier convention,
// not because it removes any artifact (see StateButtons for the real win).
const CIStatusBadge = ({ status }: { status: CIRunStatus | CIJobStatus }) => {
  return (
    <span className={clsx("ci-badge", `ci-badge--${status}`)}>{status.replace(/_/g, " ")}</span>
  )
}

export default CIStatusBadge
