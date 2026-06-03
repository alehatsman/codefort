import clsx from "clsx"
import { cloneElement, type ReactElement, type ReactNode, useId } from "react"

interface Props {
  /** The hint shown on hover/focus of the trigger. */
  label: ReactNode
  /** Placement relative to the trigger. Default "top". */
  placement?: "top" | "bottom" | "left" | "right"
  /** A single focusable trigger element (button, link, …). */
  children: ReactElement<{ "aria-describedby"?: string }>
}

/**
 * A hover/focus hint — richer than the native `title` attribute and reachable by
 * keyboard. Dependency-free: a `.tooltip` wrapper reveals its `role="tooltip"`
 * bubble on `:hover` / `:focus-within` (CSS only), and the trigger is linked to
 * it via `aria-describedby` so screen readers announce it. Wrap a single
 * focusable element; for plain non-interactive text the native `title` is fine.
 */
export default function Tooltip({ label, placement = "top", children }: Props) {
  const id = useId()
  return (
    <span className="tooltip">
      {cloneElement(children, { "aria-describedby": id })}
      <span
        id={id}
        role="tooltip"
        className={clsx("tooltip__bubble", `tooltip__bubble--${placement}`)}
      >
        {label}
      </span>
    </span>
  )
}
