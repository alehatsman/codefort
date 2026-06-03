import clsx from "clsx"
import type { ReactNode } from "react"
import { Link } from "react-router-dom"

interface Props {
  /** Destination for the whole row (the row is one big link). */
  to: string
  /** Grid column 1 — a status icon (wrap in `issue-row__icon`) or a status label. */
  leading?: ReactNode
  /** Primary line. */
  title: ReactNode
  /** Secondary line under the title. */
  meta?: ReactNode
  /** Grid column 3 — right-aligned aside (e.g. assignee). Omit to collapse it. */
  side?: ReactNode
  /** Keyboard-nav selection (hjkl): adds `is-vim-selected` + the scroll hook. */
  selected?: boolean
}

/**
 * A list row: leading slot / title + meta / side, as one link, in the
 * `.issue-row` grid. The icon/title/meta/side skeleton was hand-rolled in every
 * issue and PR list (repo + global); this is that skeleton, once. The
 * `issue-row` BEM block name is kept (not renamed to `list-row`) because it's
 * the selector contract Playwright and the vim-nav scroll hook
 * (`data-vim-selected`) target — see {@link useListNav}.
 */
export default function ListRow({ to, leading, title, meta, side, selected }: Props) {
  return (
    <li
      className={clsx("issue-row", { "is-vim-selected": selected })}
      data-vim-selected={selected ? "true" : undefined}
    >
      <Link to={to} className="issue-row__link">
        {leading}
        <span className="issue-row__main">
          <span className="issue-row__title">{title}</span>
          {meta != null && <span className="issue-row__meta">{meta}</span>}
        </span>
        {side != null && <span className="issue-row__side">{side}</span>}
      </Link>
    </li>
  )
}
