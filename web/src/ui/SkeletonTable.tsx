import clsx from "clsx"
import Skeleton from "@/ui/Skeleton"

interface Props {
  /** Number of body rows to render. */
  rows?: number
  /** Number of cells per row (match the real table's column count). */
  columns?: number
  /** Column header labels — rendered as a real `<thead>` so widths line up. */
  headers?: string[]
  /** Extra class for the `<table>` (e.g. `ci-runs` to inherit column widths). */
  className?: string
}

/**
 * A loading placeholder shaped like a {@link Table} — an optional header row
 * plus N body rows of line skeletons. Use as the `isLoading` fallback for the
 * runs / agents grids so the columns are reserved while data loads.
 */
export default function SkeletonTable({ rows = 6, columns = 4, headers, className }: Props) {
  const cols = headers?.length ?? columns
  return (
    <table className={clsx("table", className)} aria-hidden="true">
      {headers && (
        <thead>
          <tr>
            {headers.map((h) => (
              <th key={h}>{h}</th>
            ))}
          </tr>
        </thead>
      )}
      <tbody>
        {Array.from({ length: rows }, (_, r) => (
          // biome-ignore lint/suspicious/noArrayIndexKey: fixed-count static placeholder, never reorders
          <tr key={r}>
            {Array.from({ length: cols }, (_, c) => (
              // biome-ignore lint/suspicious/noArrayIndexKey: fixed-count static placeholder, never reorders
              <td key={c}>
                <Skeleton variant="line" width={c === 0 ? "70%" : "55%"} />
              </td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  )
}
