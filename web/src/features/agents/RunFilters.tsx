import { type CIRunStatus, RUN_STATUSES } from "@/api/types"
import { FilterBar, FilterChip, FilterRow } from "@/ui"

// RunFilters is the search box + status chip row shared by the global and
// per-repo Agents/Pipelines lists. State lives in the parent (useRunFilters);
// this is presentational. Pass availableRepos/activeRepos/onToggleRepo to
// show the optional repo row on fleet-wide views.
export default function RunFilters({
  search,
  onSearch,
  placeholder,
  activeStates,
  onToggleState,
  availableRepos,
  activeRepos,
  onToggleRepo,
}: {
  search: string
  onSearch: (v: string) => void
  placeholder: string
  activeStates: CIRunStatus[]
  onToggleState: (s: CIRunStatus) => void
  availableRepos?: string[]
  activeRepos?: string[]
  onToggleRepo?: (repo: string) => void
}) {
  return (
    <FilterBar
      search={search}
      onSearch={onSearch}
      searchPlaceholder={placeholder}
      searchAriaLabel="Search runs"
    >
      <FilterRow label="status:">
        {RUN_STATUSES.map((s) => (
          <FilterChip key={s} checked={activeStates.includes(s)} onChange={() => onToggleState(s)}>
            {s.replace(/_/g, " ")}
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
