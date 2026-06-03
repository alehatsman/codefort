import clsx from "clsx"
import type { ReactNode } from "react"

interface Props {
  children: ReactNode
  className?: string
}

/**
 * The detail-page rail — the `.sidebar` block: a vertical stack of
 * {@link SidebarSection}s with dividers between them. Used beside an issue/PR
 * detail body; compose sections inside it so every detail page gets the same
 * rhythm.
 */
export default function Sidebar({ children, className }: Props) {
  return <aside className={clsx("sidebar", className)}>{children}</aside>
}
