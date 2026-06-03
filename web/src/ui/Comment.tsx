import clsx from "clsx"
import type { ReactNode } from "react"
import Avatar from "@/ui/Avatar"

interface Props {
  /** Comment author — drives the avatar and the head byline. */
  author: string
  /** The byline after the author (e.g. "commented on …", "on lines …"). */
  meta?: ReactNode
  /** Head right-side slot — a resolved badge and/or action buttons. The caller
   *  owns the layout (`comment__actions` / `comment__delete`) so it can right-align. */
  actions?: ReactNode
  /** Dim the comment (the review "resolved" state) via `is-resolved`. */
  resolved?: boolean
  /** The comment body — typically lazily-rendered Markdown. */
  children: ReactNode
  /** Below the card (e.g. an inline ErrorMessage for a failed delete/resolve). */
  footer?: ReactNode
}

/**
 * The comment shell — avatar gutter + a card with a head byline and a body. The
 * `.comment` block was hand-rolled identically for issue comments and inline
 * code-review comments; this is that shell, once. Domain bits stay at the call
 * site: the body (Markdown) comes in as children, and the resolve/delete
 * controls as `actions`, so the primitive owns markup only — no mutations.
 */
export default function Comment({ author, meta, actions, resolved, children, footer }: Props) {
  return (
    <li className={clsx("comment", { "is-resolved": resolved })}>
      <span className="comment__avatar">
        <Avatar name={author} />
      </span>
      <div className="comment__card">
        <div className="comment__head">
          <strong>{author}</strong>
          {meta != null && <span className="muted">{meta}</span>}
          {actions}
        </div>
        <div className="comment__body">{children}</div>
        {footer}
      </div>
    </li>
  )
}
