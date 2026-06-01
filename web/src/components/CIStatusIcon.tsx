import type { CIRunStatus } from "../api/types"

interface Props {
  status: CIRunStatus
  size?: number
  className?: string
}

/**
 * Inline SVG CI-status icon in GitHub Primer style — the compact counterpart to
 * CIStatusBadge for dense spots like the repos-list cards. Color comes from CSS
 * via `ci-icon--<status>` (reusing the ci-badge color meanings); `currentColor`
 * lets each glyph inherit it. The status string also becomes the icon's
 * accessible label and tooltip.
 */
export default function CIStatusIcon({ status, size = 16, className = "" }: Props) {
  const cls = `ci-icon ci-icon--${status} ${className}`.trim()
  const svgProps = {
    className: cls,
    width: size,
    height: size,
    viewBox: "0 0 16 16",
    fill: "currentColor",
    role: "img",
    "aria-label": `CI ${status}`,
  }
  // success: check-circle; failed/error: x-circle; running: clock;
  // queued: open circle; canceled: circle-slash; interrupted: pause-circle.
  switch (status) {
    case "success":
      return (
        <svg {...svgProps}>
          <title>{`CI ${status}`}</title>
          <path d="M8 0a8 8 0 1 1 0 16A8 8 0 0 1 8 0Zm0 1.5a6.5 6.5 0 1 0 0 13 6.5 6.5 0 0 0 0-13Z" />
          <path d="M11.28 5.22a.75.75 0 0 1 0 1.06l-4 4a.75.75 0 0 1-1.06 0l-2-2a.75.75 0 1 1 1.06-1.06L6.75 8.69l3.47-3.47a.75.75 0 0 1 1.06 0Z" />
        </svg>
      )
    case "failed":
    case "error":
      return (
        <svg {...svgProps}>
          <title>{`CI ${status}`}</title>
          <path d="M8 0a8 8 0 1 1 0 16A8 8 0 0 1 8 0Zm0 1.5a6.5 6.5 0 1 0 0 13 6.5 6.5 0 0 0 0-13Z" />
          <path d="M5.72 5.72a.75.75 0 0 1 1.06 0L8 6.94l1.22-1.22a.75.75 0 1 1 1.06 1.06L9.06 8l1.22 1.22a.75.75 0 1 1-1.06 1.06L8 9.06l-1.22 1.22a.75.75 0 1 1-1.06-1.06L6.94 8 5.72 6.78a.75.75 0 0 1 0-1.06Z" />
        </svg>
      )
    case "running":
      return (
        <svg {...svgProps}>
          <title>{`CI ${status}`}</title>
          <path d="M8 0a8 8 0 1 1 0 16A8 8 0 0 1 8 0Zm0 1.5a6.5 6.5 0 1 0 0 13 6.5 6.5 0 0 0 0-13Z" />
          <path d="M8 4a.75.75 0 0 1 .75.75v3.19l2.03 2.03a.75.75 0 1 1-1.06 1.06L7.47 8.78A.75.75 0 0 1 7.25 8.25V4.75A.75.75 0 0 1 8 4Z" />
        </svg>
      )
    case "canceled":
      return (
        <svg {...svgProps}>
          <title>{`CI ${status}`}</title>
          <path d="M8 0a8 8 0 1 1 0 16A8 8 0 0 1 8 0Zm0 1.5a6.5 6.5 0 1 0 0 13 6.5 6.5 0 0 0 0-13Z" />
          <path d="M4.97 4.97a.75.75 0 0 1 1.06 0l4.999 5a.75.75 0 0 1-1.06 1.06l-5-5a.75.75 0 0 1 0-1.06Z" />
        </svg>
      )
    case "interrupted":
      // pause-circle — work cut short by the runner going away, not a verdict.
      return (
        <svg {...svgProps}>
          <title>{`CI ${status}`}</title>
          <path d="M8 0a8 8 0 1 1 0 16A8 8 0 0 1 8 0Zm0 1.5a6.5 6.5 0 1 0 0 13 6.5 6.5 0 0 0 0-13Z" />
          <path d="M6.25 5a.75.75 0 0 1 .75.75v4.5a.75.75 0 0 1-1.5 0v-4.5A.75.75 0 0 1 6.25 5Zm3.5 0a.75.75 0 0 1 .75.75v4.5a.75.75 0 0 1-1.5 0v-4.5A.75.75 0 0 1 9.75 5Z" />
        </svg>
      )
    default:
      // queued — open ring, awaiting a runner.
      return (
        <svg {...svgProps}>
          <title>{`CI ${status}`}</title>
          <path d="M8 9.5a1.5 1.5 0 1 0 0-3 1.5 1.5 0 0 0 0 3Z" />
          <path d="M8 0a8 8 0 1 1 0 16A8 8 0 0 1 8 0Zm0 1.5a6.5 6.5 0 1 0 0 13 6.5 6.5 0 0 0 0-13Z" />
        </svg>
      )
  }
}
