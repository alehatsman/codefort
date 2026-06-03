import "./agents.css"
import { useAllRuns } from "@/api/queries"
import AgentRunsTable from "@/features/agents/AgentRunsTable"
import RunFilters from "@/features/agents/RunFilters"
import { useRunFilters } from "@/features/agents/useRunFilters"
import { EmptyState, ErrorMessage, Spinner } from "@/ui"

// Fleet-wide Agents view: every repo's agent runs in one grid, newest-created
// first, filterable by status + keyword (like the global Issues view). Each row
// links into the owning repo's agent run detail. Replaces the old shared
// pipelines table for agents with the Agents-owned grid.
const GlobalAgentsPage = () => {
  const { search, setSearch, activeStates, toggleState, query } = useRunFilters()
  const { data, isLoading, error } = useAllRuns("agent", query)

  return (
    <section className="pipelines">
      <div className="pipelines__head">
        <h2 className="pipelines__title">Agents</h2>
      </div>
      <p className="muted small pipelines__lead">
        Every agent run across all repos. Open one to follow its transcript.
      </p>

      <RunFilters
        search={search}
        onSearch={setSearch}
        placeholder="Search commit, ref, or trigger across all repos…"
        activeStates={activeStates}
        onToggleState={toggleState}
      />

      {isLoading && <Spinner />}
      {error && <ErrorMessage error={error} />}
      {data && data.length === 0 && <EmptyState>No agent runs match these filters.</EmptyState>}
      {data && data.length > 0 && (
        <AgentRunsTable
          showRepo
          rows={data.map((run) => ({ owner: run.repo.owner, name: run.repo.name, run }))}
        />
      )}
    </section>
  )
}

export default GlobalAgentsPage
