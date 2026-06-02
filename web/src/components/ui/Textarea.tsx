import clsx from "clsx"
import type { TextareaHTMLAttributes } from "react"

/**
 * Monospace, vertically-resizable textarea — the `.textarea` block. Defaults
 * the class and forwards native props (7 raw usages: comment/issue/review
 * compose boxes).
 */
export default function Textarea({
  className,
  ...rest
}: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return <textarea className={clsx("textarea", className)} {...rest} />
}
