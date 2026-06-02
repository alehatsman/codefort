import { absoluteTime, timeAgo } from "@/shell/timeAgo"

interface Props {
  /** ISO-8601 timestamp. */
  iso: string
  /** Optional className for the wrapping `<span>` (e.g. `"muted small"`). */
  className?: string
}

/**
 * A relative timestamp with the absolute time on hover — the
 * `<span title={absoluteTime(iso)}>{timeAgo(iso)}</span>` pair repeated at ~12
 * call sites (commit/issue/run activity lines). Pairs the two lib/timeAgo
 * helpers so call sites stop importing both.
 */
export default function RelativeTime({ iso, className }: Props) {
  return (
    <span className={className} title={absoluteTime(iso)}>
      {timeAgo(iso)}
    </span>
  )
}
