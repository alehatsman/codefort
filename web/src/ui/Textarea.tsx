import clsx from "clsx"
import type { ComponentPropsWithRef } from "react"

/**
 * Monospace, vertically-resizable textarea — the `.textarea` block. Defaults
 * the class and forwards native props (incl. `ref`): comment/issue/review
 * compose boxes.
 */
export default function Textarea({ className, ...rest }: ComponentPropsWithRef<"textarea">) {
  return <textarea className={clsx("textarea", className)} {...rest} />
}
