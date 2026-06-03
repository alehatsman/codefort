import Skeleton from "@/ui/Skeleton"

interface Props {
  /** How many row placeholders to render. */
  rows?: number
  /** Reserve the leading icon/status slot (matches ListRow's `leading`). */
  leading?: boolean
}

/**
 * A loading placeholder shaped like a {@link ListRow} list — the `.issue-list`
 * block of N rows, each an optional leading dot + a title line + a shorter meta
 * line. Use as the `isLoading` fallback for issue / PR list pages so the page
 * reserves the eventual list's shape instead of jumping from a centered spinner.
 */
export default function SkeletonList({ rows = 6, leading = true }: Props) {
  return (
    <ul className="issue-list" aria-hidden="true">
      {Array.from({ length: rows }, (_, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: fixed-count static placeholder, never reorders
        <li key={i} className="issue-row">
          <div className="issue-row__link">
            {leading && (
              <span className="issue-row__icon">
                <Skeleton variant="circle" width={16} height={16} />
              </span>
            )}
            <span className="issue-row__main">
              <Skeleton variant="line" width="55%" />
              <span className="issue-row__meta">
                <Skeleton variant="line" width="40%" />
              </span>
            </span>
          </div>
        </li>
      ))}
    </ul>
  )
}
