import type { KeyboardEvent, ReactNode } from "react"

interface FilterBarProps {
  /** Current keyword search value. Omit to hide the search input. */
  search?: string
  onSearch?: (value: string) => void
  searchPlaceholder?: string
  searchAriaLabel?: string
  onSearchKeyDown?: (e: KeyboardEvent<HTMLInputElement>) => void
  children: ReactNode
}

/**
 * The `.filters` block — bordered card wrapping a keyword search input (opt-in)
 * and one or more {@link FilterRow}s. Replaces the hand-rolled
 * `<div className="filters">` in IssuesPage, PullsFilters, RunFilters, etc.
 */
export function FilterBar({
  search,
  onSearch,
  searchPlaceholder = "Search…",
  searchAriaLabel = "Search",
  onSearchKeyDown,
  children,
}: FilterBarProps) {
  return (
    <div className="filters">
      {search !== undefined && onSearch !== undefined && (
        <input
          type="search"
          className="list-search"
          placeholder={searchPlaceholder}
          value={search}
          onChange={(e) => onSearch(e.target.value)}
          onKeyDown={onSearchKeyDown}
          aria-label={searchAriaLabel}
        />
      )}
      {children}
    </div>
  )
}

interface FilterRowProps {
  /** Leading label text, e.g. "state:" or "status:". */
  label?: string
  children: ReactNode
}

/**
 * A `.filter-row` — a horizontal strip of chips, selects, or other controls,
 * optionally preceded by a muted `.filter-label`. Composable inside
 * {@link FilterBar}.
 */
export function FilterRow({ label, children }: FilterRowProps) {
  return (
    <div className="filter-row">
      {label && <span className="filter-label">{label}</span>}
      {children}
    </div>
  )
}
