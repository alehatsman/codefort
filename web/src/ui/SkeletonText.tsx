import Skeleton from "@/ui/Skeleton"
import Stack from "@/ui/Stack"

interface Props {
  /** Render a wider, taller heading line above the body lines. */
  heading?: boolean
  /** How many body text lines to render. */
  lines?: number
}

/**
 * A loading placeholder shaped like a detail / prose block — an optional
 * heading line over N body lines of varying width. Use as the `isLoading`
 * fallback for detail pages (commit, pull, issue) so the page reserves the
 * eventual header + body shape instead of a centered spinner.
 */
export default function SkeletonText({ heading = true, lines = 3 }: Props) {
  return (
    <Stack gap={2} aria-hidden="true">
      {heading && <Skeleton variant="line" width="45%" height="1.4em" />}
      {Array.from({ length: lines }, (_, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: fixed-count static placeholder, never reorders
        <Skeleton key={i} variant="line" width={i === lines - 1 ? "60%" : "100%"} />
      ))}
    </Stack>
  )
}
