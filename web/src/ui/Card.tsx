import clsx from "clsx"
import { forwardRef, type HTMLAttributes } from "react"

interface Props extends HTMLAttributes<HTMLDivElement> {
  /** Toggles the `.is-vim-selected` highlight used by keyboard nav lists. */
  selected?: boolean
}

/**
 * A surface container — the `.card` BEM block. Thin by design: it owns no
 * layout beyond the shared card styling, so callers compose their own inner
 * markup. Link-style cards (a whole card that navigates) stay as
 * `<Link className="card">` since they need router semantics this div can't
 * carry; this covers the plain `<div className="card">` case (~38 usages).
 *
 * forwardRef so drag-and-drop libraries (dnd-kit) can attach their node refs.
 */
const Card = forwardRef<HTMLDivElement, Props>(function Card(
  { selected, className, ...rest },
  ref
) {
  return (
    <div
      ref={ref}
      className={clsx("card", { "is-vim-selected": selected }, className)}
      {...rest}
    />
  )
})
export default Card
