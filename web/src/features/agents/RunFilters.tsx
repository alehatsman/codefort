import { type CIRunStatus, RUN_STATUSES } from "@/api/types"
import { FilterBar, FilterChip, FilterRow } from "@/ui"

// RunFilters is the search box + status chip row shared by the global and
// per-repo Agents lists. State lives in the parent (useRunFilters); this is
// presentational.
export default function RunFilters({
  search,
  onSearch,
  placeholder,
  activeStates,
  onToggleState,
}: {
  search: string
  onSearch: (v: string) => void
  placeholder: string
  activeStates: CIRunStatus[]
  onToggleState: (s: CIRunStatus) => void
}) {
  return (
    <FilterBar
      search={search}
      onSearch={onSearch}
      searchPlaceholder={placeholder}
      searchAriaLabel="Search agent runs"
    >
      <FilterRow label="status:">
        {RUN_STATUSES.map((s) => (
          <FilterChip key={s} checked={activeStates.includes(s)} onChange={() => onToggleState(s)}>
            {s.replace(/_/g, " ")}
          </FilterChip>
        ))}
      </FilterRow>
    </FilterBar>
  )
}
