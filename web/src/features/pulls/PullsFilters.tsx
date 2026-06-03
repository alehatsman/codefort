import { PR_STATES, type PRState } from "@/api/types"
import PRStateIcon from "@/features/pulls/PRStateIcon"
import { FilterBar, FilterChip, FilterRow } from "@/ui"

interface Props {
  search: string
  onSearchChange: (value: string) => void
  searchPlaceholder?: string
  activeStates: PRState[]
  onToggleState: (state: PRState) => void
}

/**
 * PR list filter bar — keyword search + state chips — shared by PullsPage and
 * GlobalPullsPage. State is owned by the caller; this is purely presentational.
 */
export default function PullsFilters({
  search,
  onSearchChange,
  searchPlaceholder = "Search title or body…",
  activeStates,
  onToggleState,
}: Props) {
  return (
    <FilterBar
      search={search}
      onSearch={onSearchChange}
      searchPlaceholder={searchPlaceholder}
      searchAriaLabel="Search pull requests"
    >
      <FilterRow label="state:">
        {PR_STATES.map((s) => (
          <FilterChip key={s} checked={activeStates.includes(s)} onChange={() => onToggleState(s)}>
            <PRStateIcon state={s} size={12} />
            {s}
          </FilterChip>
        ))}
      </FilterRow>
    </FilterBar>
  )
}
