import clsx from "clsx"
import type { ComponentPropsWithRef } from "react"

/**
 * A data table — the `.table` block: full width, collapsed borders, a muted
 * header row and per-row dividers. Compose native `<thead>/<tbody>/<tr>/<th>/
 * <td>` inside; `.table th` / `.table td` carry the density so cells stay plain
 * markup. Pass `className` for a per-table block (column widths, alignment) —
 * e.g. the runs grids extend it as `.ci-runs` / `.agent-runs`.
 */
export default function Table({ className, ...rest }: ComponentPropsWithRef<"table">) {
  return <table className={clsx("table", className)} {...rest} />
}
