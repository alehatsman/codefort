import clsx from "clsx"
import type { ComponentPropsWithRef, ReactNode } from "react"

interface Props extends ComponentPropsWithRef<"input"> {
  /** Text shown beside the box; also the control's accessible name. */
  label?: ReactNode
}

/**
 * A labelled checkbox — the `.control` family (native input, accent-colored).
 * The `<label>` wraps the input so the text is its accessible name with no
 * `htmlFor` wiring. Forwards every native prop (incl. `ref`); pass `className`
 * to extend the wrapper. See {@link Radio} (same family) and {@link Switch}.
 */
export default function Checkbox({ label, className, ...rest }: Props) {
  return (
    <label className={clsx("control", className)}>
      <input type="checkbox" className="control__input" {...rest} />
      {label != null && <span className="control__text">{label}</span>}
    </label>
  )
}
