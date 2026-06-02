import clsx from "clsx"
import type { ButtonHTMLAttributes } from "react"

export type ButtonVariant = "default" | "primary" | "ghost" | "danger"
export type ButtonSize = "md" | "small"

interface Props extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant
  size?: ButtonSize
}

/**
 * The base button. Wraps the `.btn` BEM block and its variant/size modifiers
 * so call sites pick from a typed `variant`/`size` instead of hand-stitching
 * class strings (the repo had 47 raw `btn` usages, plus a dead `btn--ghost`
 * and a near-duplicate `btn--sm`/`btn--small` pair — see styles.css).
 *
 * Defaults to `type="button"`: a bare <button> inside a <form> submits, which
 * is almost never what these are for. Pass `type="submit"` explicitly.
 */
export default function Button({
  variant = "default",
  size = "md",
  className,
  type = "button",
  ...rest
}: Props) {
  return (
    <button
      type={type}
      className={clsx(
        "btn",
        {
          "btn--primary": variant === "primary",
          "btn--ghost": variant === "ghost",
          "btn--danger": variant === "danger",
          "btn--small": size === "small",
        },
        className
      )}
      {...rest}
    />
  )
}
