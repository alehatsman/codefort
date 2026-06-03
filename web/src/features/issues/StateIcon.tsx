import type { IssueState } from "@/api/types"

interface Props {
  state: IssueState
  size?: number
  className?: string
}

/**
 * Inline SVG state icons in GitHub Primer style. Color comes from CSS
 * via `state-icon--<state>` modifier; `currentColor` lets the icon
 * inherit it.
 */
export default function StateIcon({ state, size = 16, className = "" }: Props) {
  const cls = `state-icon state-icon--${state} ${className}`.trim()
  const svgProps = {
    className: cls,
    width: size,
    height: size,
    viewBox: "0 0 16 16",
    fill: "currentColor",
    "aria-label": state,
  }
  // Open circle for todo; clock-ish for in_progress; check-circle for done;
  // skip-circle for closed.
  switch (state) {
    case "todo":
      return (
        <svg {...svgProps}>
          <title>{state}</title>
          <path d="M8 9.5a1.5 1.5 0 1 0 0-3 1.5 1.5 0 0 0 0 3Z" />
          <path d="M8 0a8 8 0 1 1 0 16A8 8 0 0 1 8 0Zm0 1.5a6.5 6.5 0 1 0 0 13 6.5 6.5 0 0 0 0-13Z" />
        </svg>
      )
    case "in_progress":
      return (
        <svg {...svgProps}>
          <title>{state}</title>
          <path d="M8 0a8 8 0 1 1 0 16A8 8 0 0 1 8 0Zm0 1.5a6.5 6.5 0 1 0 0 13 6.5 6.5 0 0 0 0-13Z" />
          <path d="M8 4a.75.75 0 0 1 .75.75v3.19l2.03 2.03a.75.75 0 1 1-1.06 1.06L7.47 8.78A.75.75 0 0 1 7.25 8.25V4.75A.75.75 0 0 1 8 4Z" />
        </svg>
      )
    case "done":
      return (
        <svg {...svgProps}>
          <title>{state}</title>
          <path d="M8 0a8 8 0 1 1 0 16A8 8 0 0 1 8 0Zm0 1.5a6.5 6.5 0 1 0 0 13 6.5 6.5 0 0 0 0-13Z" />
          <path d="M11.28 5.22a.75.75 0 0 1 0 1.06l-4 4a.75.75 0 0 1-1.06 0l-2-2a.75.75 0 1 1 1.06-1.06L6.75 8.69l3.47-3.47a.75.75 0 0 1 1.06 0Z" />
        </svg>
      )
    case "closed":
      return (
        <svg {...svgProps}>
          <title>{state}</title>
          <path d="M8 0a8 8 0 1 1 0 16A8 8 0 0 1 8 0Zm0 1.5a6.5 6.5 0 1 0 0 13 6.5 6.5 0 0 0 0-13Z" />
          <path d="M5.72 5.72a.75.75 0 0 1 1.06 0L8 6.94l1.22-1.22a.75.75 0 1 1 1.06 1.06L9.06 8l1.22 1.22a.75.75 0 1 1-1.06 1.06L8 9.06l-1.22 1.22a.75.75 0 1 1-1.06-1.06L6.94 8 5.72 6.78a.75.75 0 0 1 0-1.06Z" />
        </svg>
      )
  }
}
