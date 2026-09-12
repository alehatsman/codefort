import clsx from "clsx"
import type { ReactNode } from "react"

interface Props {
  children: ReactNode
  /**
   * Draws the bordered card frame around the message. Replaces the inline
   * `{ border, borderRadius }` style that was copy-pasted at a few call sites
   * onto the bare `.empty` block.
   */
  bordered?: boolean
  className?: string
}

/**
 * The muted, centered "nothing here" placeholder — the `.empty` block, used
 * ~34 times for empty lists and not-found states.
 */
export default function EmptyState({ children, bordered, className }: Props) {
  return <div className={clsx("empty", { "empty--bordered": bordered }, className)}>{children}</div>
}
