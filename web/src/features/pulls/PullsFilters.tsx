import { PR_STATES, type PRState } from "@/api/types"
import PRStateIcon from "@/features/pulls/PRStateIcon"
import { FilterBar, FilterChip, FilterRow } from "@/ui"

interface Props {
  search: string
  onSearchChange: (value: string) => void
  searchPlaceholder?: string
  activeStates: PRState[]
  onToggleState: (state: PRState) => void
  availableRepos?: string[]
  activeRepos?: string[]
  onToggleRepo?: (repo: string) => void
}

// PR list filter bar — keyword search + state chips — shared by PullsPage and
// GlobalPullsPage. Pass availableRepos/activeRepos/onToggleRepo to show the
// repo row on fleet-wide views. State is owned by the caller.
export default function PullsFilters({
  search,
  onSearchChange,
  searchPlaceholder = "Search title or body…",
  activeStates,
  onToggleState,
  availableRepos,
  activeRepos,
  onToggleRepo,
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
      {availableRepos && availableRepos.length > 0 && onToggleRepo && (
        <FilterRow label="repo:">
          {availableRepos.map((r) => (
            <FilterChip
              key={r}
              checked={activeRepos?.includes(r) ?? false}
              onChange={() => onToggleRepo(r)}
            >
              {r}
            </FilterChip>
          ))}
        </FilterRow>
      )}
    </FilterBar>
  )
}
