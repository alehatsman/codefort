import clsx from "clsx"
import type { ComponentPropsWithRef, ReactNode } from "react"

interface Props extends ComponentPropsWithRef<"input"> {
  /** Text shown beside the toggle; also the control's accessible name. */
  label?: ReactNode
}

/**
 * An on/off toggle — a native checkbox styled as a sliding switch (the `.switch`
 * block), wrapped in the shared `.control` label. Use for binary settings where
 * a toggle reads better than a checkbox; the underlying input is still a
 * checkbox, so it forwards every native prop (incl. `ref`) and `checked`/`onChange`.
 */
export default function Switch({ label, className, ...rest }: Props) {
  return (
    <label className={clsx("control", className)}>
      <span className="switch">
        <input type="checkbox" className="switch__input" {...rest} />
        <span className="switch__track" />
        <span className="switch__thumb" />
      </span>
      {label != null && <span className="control__text">{label}</span>}
    </label>
  )
}
