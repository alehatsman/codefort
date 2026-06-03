import clsx from "clsx"
import type { ReactNode } from "react"
import StatusPill from "@/ui/StatusPill"

/** Matches the `.badge--<state>` color modifiers in styles.css. */
export type BadgeState = "todo" | "in_progress" | "done" | "closed"

interface Props {
  children: ReactNode
  /** Color modifier. Omit for the neutral pill (e.g. the "you" tag). */
  state?: BadgeState
  className?: string
}

/**
 * The issue-state pill — a thin map over {@link StatusPill} that supplies the
 * `.badge--<state>` color modifier (todo / in_progress / done / closed). Omit
 * `state` for the neutral pill. For CI run/job statuses use CIStatusBadge,
 * which maps its richer status vocabulary onto the same shell.
 */
export default function Badge({ children, state, className }: Props) {
  return <StatusPill className={clsx(state && `badge--${state}`, className)}>{children}</StatusPill>
}
