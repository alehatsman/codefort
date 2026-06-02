import clsx from "clsx"
import type { SelectHTMLAttributes } from "react"

/**
 * Native select styled as the `.select` block. Defaults the class and forwards
 * native props, including `children` for the <option> list (12 raw usages).
 */
export default function Select({ className, ...rest }: SelectHTMLAttributes<HTMLSelectElement>) {
  return <select className={clsx("select", className)} {...rest} />
}
