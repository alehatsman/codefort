import type { IssueState } from "@/api/types"
import StatusIcon, { type StatusGlyph } from "@/ui/StatusIcon"

// Open ring for todo; clock for in_progress; check for done; × for closed.
const GLYPH: Record<IssueState, StatusGlyph> = {
  todo: "dot-ring",
  in_progress: "clock",
  done: "check",
  closed: "x",
}

interface Props {
  state: IssueState
  size?: number
  className?: string
}

/**
 * Issue-state icon. A thin map from issue state to a StatusIcon glyph; color
 * comes from CSS via the `state-icon--<state>` modifier, which `currentColor`
 * inherits.
 */
export default function StateIcon({ state, size = 16, className = "" }: Props) {
  return (
    <StatusIcon
      glyph={GLYPH[state]}
      label={state}
      size={size}
      className={`state-icon state-icon--${state} ${className}`.trim()}
    />
  )
}
