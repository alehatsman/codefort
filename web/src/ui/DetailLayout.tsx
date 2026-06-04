import clsx from "clsx"
import type { ReactNode } from "react"
import Sidebar from "@/ui/Sidebar"

interface Props {
  /** Optional sidebar column content. Wrapped in Sidebar automatically. */
  sidebar?: ReactNode
  children: ReactNode
  className?: string
}

/**
 * Detail-page shell: left main column + optional right Sidebar rail, in a
 * 1fr/280px grid. Use for issue/PR detail pages that show metadata in a
 * sidebar alongside the primary content. When sidebar is omitted the grid
 * collapses to a single column.
 */
export default function DetailLayout({ sidebar, children, className }: Props) {
  return (
    <div
      className={clsx(
        "detail-layout__body",
        { "detail-layout__body--nosidebar": !sidebar },
        className
      )}
    >
      <div className="detail-layout__main">{children}</div>
      {sidebar && <Sidebar>{sidebar}</Sidebar>}
    </div>
  )
}
