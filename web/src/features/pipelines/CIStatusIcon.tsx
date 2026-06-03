import type { CIRunStatus } from "@/api/types"
import StatusIcon, { type StatusGlyph } from "@/ui/StatusIcon"

// success: check; failed/error: ×; running: clock; canceled: slash;
// stalled: alert (ran to completion but never converged); interrupted: pause
// (runner went away). queued — and the agent-only awaiting_input / finishing
// live states — show the open ring, matching the prior `default` branch.
const GLYPH: Record<CIRunStatus, StatusGlyph> = {
  queued: "dot-ring",
  running: "clock",
  awaiting_input: "dot-ring",
  finishing: "dot-ring",
  success: "check",
  failed: "x",
  canceled: "slash",
  error: "x",
  interrupted: "pause",
  stalled: "alert",
}

interface Props {
  status: CIRunStatus
  size?: number
  className?: string
}

/**
 * Inline SVG CI-status icon in GitHub Primer style — the compact counterpart to
 * CIStatusBadge for dense spots like the repos-list cards. A thin map from run
 * status to a StatusIcon glyph; color comes from CSS via `ci-icon--<status>`
 * (reusing the ci-badge color meanings), which `currentColor` inherits. The
 * status string also becomes the icon's accessible label and tooltip.
 */
export default function CIStatusIcon({ status, size = 16, className = "" }: Props) {
  return (
    <StatusIcon
      glyph={GLYPH[status]}
      label={`CI ${status}`}
      size={size}
      className={`ci-icon ci-icon--${status} ${className}`.trim()}
    />
  )
}
