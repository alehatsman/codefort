import clsx from "clsx"
import type { ComponentPropsWithRef, ReactNode } from "react"

interface Props extends ComponentPropsWithRef<"input"> {
  /** Text shown beside the dot; also the control's accessible name. */
  label?: ReactNode
}

/**
 * A labelled radio button — the `.control` family, sharing layout + the native
 * accent-colored input with {@link Checkbox}. Group members by passing the same
 * `name`. The `<label>` wraps the input so the text is its accessible name.
 */
export default function Radio({ label, className, ...rest }: Props) {
  return (
    <label className={clsx("control", className)}>
      <input type="radio" className="control__input" {...rest} />
      {label != null && <span className="control__text">{label}</span>}
    </label>
  )
}
