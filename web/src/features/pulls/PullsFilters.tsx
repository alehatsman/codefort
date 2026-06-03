import { PR_STATES, type PRState } from "@/api/types"
import PRStateIcon from "@/features/pulls/PRStateIcon"
import { FilterChip } from "@/ui"

interface Props {
  search: string
  onSearchChange: (value: string) => void
  searchPlaceholder?: string
  activeStates: PRState[]
  onToggleState: (state: PRState) => void
}

/**
 * The PR list `.filters` block — keyword search + state chips — shared by the
 * per-repo PullsPage and the fleet-wide GlobalPullsPage so both render the same
 * FilterChip + PRStateIcon row (the global page used bare checkboxes with no
 * icons before this). The owning page keeps its own URL-param state and passes
 * the value + handlers down; this is purely presentational.
 */
export default function PullsFilters({
  search,
  onSearchChange,
  searchPlaceholder = "Search title or body…",
  activeStates,
  onToggleState,
}: Props) {
  return (
    <div className="filters">
      <input
        type="search"
        className="list-search"
        placeholder={searchPlaceholder}
        value={search}
        onChange={(e) => onSearchChange(e.target.value)}
        aria-label="Search pull requests"
      />
      <div className="filter-row">
        <span className="filter-label">state:</span>
        {PR_STATES.map((s) => (
          <FilterChip key={s} checked={activeStates.includes(s)} onChange={() => onToggleState(s)}>
            <PRStateIcon state={s} size={12} />
            {s}
          </FilterChip>
        ))}
      </div>
    </div>
  )
}
