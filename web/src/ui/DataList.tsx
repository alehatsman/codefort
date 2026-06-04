import type { ReactNode } from "react"
import DataState from "@/ui/DataState"
import SkeletonList from "@/ui/SkeletonList"

interface Props<T> {
  data: T[] | null | undefined
  isLoading: boolean
  error: unknown
  /** Loading placeholder — defaults to SkeletonList. */
  skeleton?: ReactNode
  /** Shown when the array is empty. */
  empty: ReactNode
  /**
   * Render the non-empty list. Return {@link ListRow} elements (each renders
   * a `<li>`); DataList wraps them in `<ul className="issue-list">`.
   */
  children: (data: T[]) => ReactNode
}

/**
 * DataState specialised for issue-/PR-style list pages. Defaults the loading
 * skeleton to SkeletonList and wraps the data render in
 * `<ul className="issue-list">` so call sites only describe what each item
 * looks like, not the four-state scaffold around it.
 */
export default function DataList<T>({
  data,
  isLoading,
  error,
  skeleton = <SkeletonList />,
  empty,
  children,
}: Props<T>) {
  return (
    <DataState
      data={data}
      isLoading={isLoading}
      error={error}
      skeleton={skeleton}
      empty={empty}
    >
      {(items) => <ul className="issue-list">{children(items)}</ul>}
    </DataState>
  )
}
