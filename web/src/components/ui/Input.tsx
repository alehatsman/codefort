import clsx from "clsx"
import type { InputHTMLAttributes } from "react"

/**
 * Text-style input — the `.input` block. A thin wrapper that defaults the
 * class and forwards every native prop, so call sites stop repeating
 * `className="input"` (26 raw usages). Pass `className` to extend, not replace.
 */
export default function Input({ className, ...rest }: InputHTMLAttributes<HTMLInputElement>) {
  return <input className={clsx("input", className)} {...rest} />
}
