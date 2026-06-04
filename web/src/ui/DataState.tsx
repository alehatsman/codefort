import type { ReactNode } from "react"
import EmptyState from "@/ui/EmptyState"
import ErrorMessage from "@/ui/ErrorMessage"

interface Props<T> {
  data: T[] | null | undefined
  isLoading: boolean
  error: unknown
  /** Shown while loading. */
  skeleton: ReactNode
  /** Shown when data is an empty array. */
  empty: ReactNode
  /** Called with the non-empty array once loaded. */
  children: (data: T[]) => ReactNode
}

/**
 * Four-state gate: loading → error → empty → data. Eliminates the repeated
 * `{isLoading && <Skel />}{error && <Err />}{data?.length === 0 && <Empty />}`
 * ladder that every list/table page hand-rolls.
 */
export default function DataState<T>({
  data,
  isLoading,
  error,
  skeleton,
  empty,
  children,
}: Props<T>) {
  if (isLoading) return <>{skeleton}</>
  if (error) return <ErrorMessage error={error} />
  if (!data || data.length === 0) return <EmptyState>{empty}</EmptyState>
  return <>{children(data)}</>
}
