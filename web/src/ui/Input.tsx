import clsx from "clsx"
import type { ComponentPropsWithRef } from "react"

/**
 * Text-style input — the `.input` block. A thin wrapper that defaults the
 * class and forwards every native prop (incl. `ref`), so call sites stop
 * repeating `className="input"`. Pass `className` to extend, not replace.
 */
export default function Input({ className, ...rest }: ComponentPropsWithRef<"input">) {
  return <input className={clsx("input", className)} {...rest} />
}
