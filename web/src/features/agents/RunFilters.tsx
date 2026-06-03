import { type CIRunStatus, RUN_STATUSES } from "@/api/types"

// RunFilters is the search box + status chip row shared by the global and
// per-repo Agents lists, matching the issues filter chrome (.filters/.chip).
// State lives in the parent (useRunFilters); this is presentational.
const RunFilters = ({
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
}) => {
  return (
    <div className="filters">
      <input
        type="search"
        className="issues__search"
        placeholder={placeholder}
        value={search}
        onChange={(e) => onSearch(e.target.value)}
        aria-label="Search agent runs"
      />
      <div className="filter-row">
        <span className="filter-label">status:</span>
        {RUN_STATUSES.map((s) => (
          <label key={s} className="chip">
            <input
              type="checkbox"
              checked={activeStates.includes(s)}
              onChange={() => onToggleState(s)}
            />
            {s.replace(/_/g, " ")}
          </label>
        ))}
      </div>
    </div>
  )
}

export default RunFilters
