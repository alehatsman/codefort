import clsx from "clsx"
import type { ReactNode } from "react"

interface Props {
  children: ReactNode
  /** Tighter padding, no gap — the compact CI-status look. */
  dense?: boolean
  /** Capitalize the label (CI statuses come through lower-case). */
  capitalize?: boolean
  /** The color modifier — passed by the domain wrapper (e.g. `badge--done`,
   *  `ci-badge--failed`), so the pill itself stays color-agnostic. */
  className?: string
}

/**
 * The shared colored-pill shell — one `.pill` block behind both {@link Badge}
 * (issue states) and CIStatusBadge (CI run/job statuses). Those two were
 * parallel `.badge` / `.ci-badge` blocks duplicating the same shape; this is
 * that shape, once. Color stays domain-owned: each wrapper passes its
 * `*--<status>` modifier via `className`, so the colors (and their existing
 * CSS + test selectors) are untouched. `dense`/`capitalize` carry the only two
 * presentational differences the CI variant needs.
 */
export default function StatusPill({ children, dense, capitalize, className }: Props) {
  return (
    <span
      className={clsx("pill", { "pill--dense": dense, "pill--capitalize": capitalize }, className)}
    >
      {children}
    </span>
  )
}
