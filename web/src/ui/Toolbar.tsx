import clsx from "clsx"
import type { ReactNode } from "react"
import Inline from "@/ui/Inline"

interface Props {
  /** Accessible name for the toolbar region. */
  label: string
  /** Wrap controls onto multiple lines when they overflow. Default true. */
  wrap?: boolean
  /** Render as a bordered card (the list-filter look) vs. a bare control row. */
  card?: boolean
  className?: string
  children: ReactNode
}

/**
 * A horizontal bar of controls — filters, view switches, action buttons. Wraps
 * {@link Inline} with `role="toolbar"` and an optional bordered-card surface
 * (the `.filters` look). Use for the filter/action strip above a list.
 */
export default function Toolbar({ label, wrap = true, card, className, children }: Props) {
  return (
    <Inline
      as="div"
      role="toolbar"
      aria-label={label}
      gap={3}
      wrap={wrap}
      className={clsx("toolbar", { "toolbar--card": card }, className)}
    >
      {children}
    </Inline>
  )
}
