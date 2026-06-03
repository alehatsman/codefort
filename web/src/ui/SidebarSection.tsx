import type { ReactNode } from "react"

interface Props {
  /** The section heading (rendered as the `.sidebar__label` h3). */
  label: ReactNode
  children: ReactNode
}

/**
 * One labelled section of a {@link Sidebar} — a heading over its content, with a
 * divider below (the last section's is suppressed by CSS). The `<h3>` keeps the
 * detail rail's heading hierarchy.
 */
export default function SidebarSection({ label, children }: Props) {
  return (
    <section className="sidebar__section">
      <h3 className="sidebar__label">{label}</h3>
      {children}
    </section>
  )
}
