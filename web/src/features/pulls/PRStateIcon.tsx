import type { PRState } from "@/api/types"
import StateIcon from "@/features/issues/StateIcon"

interface Props {
  state: PRState
  size?: number
  className?: string
}

/**
 * State icon for pull requests. open/closed reuse the issue glyphs
 * (open-circle / x-circle) since the colors already line up; merged is
 * unique to PRs so it gets its own git-merge glyph in Primer style.
 */
const PRStateIcon = ({ state, size = 16, className = "" }: Props) => {
  if (state !== "merged") {
    // open → todo's open circle (green), closed → closed's x-circle (red).
    return (
      <StateIcon state={state === "open" ? "todo" : "closed"} size={size} className={className} />
    )
  }
  const cls = `state-icon state-icon--merged ${className}`.trim()
  return (
    <svg
      className={cls}
      width={size}
      height={size}
      viewBox="0 0 16 16"
      fill="currentColor"
      aria-label="merged"
    >
      <title>merged</title>
      <path d="M5.45 5.154A4.25 4.25 0 0 0 9.25 7.5h1.378a2.251 2.251 0 1 1 0 1.5H9.25A5.734 5.734 0 0 1 5 7.123v3.505a2.25 2.25 0 1 1-1.5 0V5.372a2.25 2.25 0 1 1 1.95-.218ZM4.25 13.5a.75.75 0 1 0 0-1.5.75.75 0 0 0 0 1.5Zm8.5-4.5a.75.75 0 1 0 0-1.5.75.75 0 0 0 0 1.5ZM5 3.25a.75.75 0 1 0-1.5 0 .75.75 0 0 0 1.5 0Z" />
    </svg>
  )
}

export default PRStateIcon
