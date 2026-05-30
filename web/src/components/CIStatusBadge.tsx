import type { CIJobStatus, CIRunStatus } from "../api/types"

// CIStatusBadge renders a run or job status as a colored pill. Colors are
// driven by CSS (`ci-badge--<status>`) so they track the active theme; the
// status strings are shared between runs and jobs (jobs add "skipped").
export default function CIStatusBadge({ status }: { status: CIRunStatus | CIJobStatus }) {
  return <span className={`ci-badge ci-badge--${status}`}>{status}</span>
}
