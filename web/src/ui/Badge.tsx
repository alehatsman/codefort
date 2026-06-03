import clsx from "clsx"
import type { ReactNode } from "react"

/** Matches the `.badge--<state>` color modifiers in styles.css. */
export type BadgeState = "todo" | "in_progress" | "done" | "closed"

interface Props {
  children: ReactNode
  /** Color modifier. Omit for the neutral badge (e.g. the "you" tag). */
  state?: BadgeState
  className?: string
}

/**
 * A small status pill — the `.badge` block. The optional `state` drives the
 * themed color modifier (todo / in_progress / done / closed). For CI run/job
 * statuses use CIStatusBadge, which has its own richer status vocabulary.
 */
const Badge = ({ children, state, className }: Props) => {
  return <span className={clsx("badge", state && `badge--${state}`, className)}>{children}</span>
}

export default Badge
