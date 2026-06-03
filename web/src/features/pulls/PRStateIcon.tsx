import type { PRState } from "@/api/types"
import StateIcon from "@/features/issues/StateIcon"
import StatusIcon from "@/ui/StatusIcon"

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
export default function PRStateIcon({ state, size = 16, className = "" }: Props) {
  if (state !== "merged") {
    // open → todo's open circle (green), closed → closed's x-circle (red).
    return (
      <StateIcon state={state === "open" ? "todo" : "closed"} size={size} className={className} />
    )
  }
  return (
    <StatusIcon
      glyph="merge"
      label="merged"
      size={size}
      className={`state-icon state-icon--merged ${className}`.trim()}
    />
  )
}
