import clsx from "clsx"
import type { ReactNode } from "react"
import Inline from "@/ui/Inline"

interface Props {
  /** The page heading. A string renders as the `<h2>` title; a node is used verbatim. */
  title: ReactNode
  /** Right-aligned actions (buttons, new-item forms). */
  actions?: ReactNode
  /** Extra content beside the title (e.g. a view switcher). */
  children?: ReactNode
  className?: string
}

/**
 * Standard page header: a title (with optional sibling controls) on the left and
 * actions on the right, separated to the edges. Replaces the per-page
 * `issues__header` / `issues__header-left` markup that every list page hand-rolled.
 * Composed from {@link Inline} so the lead/actions groups share the spacing scale.
 */
export default function PageHeader({ title, actions, children, className }: Props) {
  return (
    <Inline as="header" justify="between" gap={4} className={clsx("page-header", className)}>
      <Inline gap={4} className="page-header__lead">
        {typeof title === "string" ? <h2 className="page-header__title">{title}</h2> : title}
        {children}
      </Inline>
      {actions != null && <Inline className="page-header__actions">{actions}</Inline>}
    </Inline>
  )
}
