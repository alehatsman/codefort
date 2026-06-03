import clsx from "clsx"
import type { ComponentPropsWithRef } from "react"

/**
 * Native select styled as the `.select` block. Defaults the class and forwards
 * native props (incl. `ref`) and `children` for the <option> list.
 */
const Select = ({ className, ...rest }: ComponentPropsWithRef<"select">) => {
  return <select className={clsx("select", className)} {...rest} />
}

export default Select
