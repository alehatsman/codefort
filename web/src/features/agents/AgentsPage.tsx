import "./agents.css"
import { useParams } from "react-router-dom"
import { useCIRuns } from "@/api/queries"
import AgentRunsTable from "@/features/agents/AgentRunsTable"
import RunFilters from "@/features/agents/RunFilters"
import { useRunFilters } from "@/features/agents/useRunFilters"
import { EmptyState, ErrorMessage, Spinner } from "@/ui"

// Per-repo Agents tab list. Agent runs are spawned from issues regardless of the
// CI opt-in, so (unlike Pipelines) there's no CI-disabled gate. Status + keyword
// filters mirror the per-repo Issues page; the run detail stays on the shared
// run shell (PipelinesPage), reached via the grid's #number links.
const AgentsPage = () => {
  const { owner = "", repo = "" } = useParams()
  const { search, setSearch, activeStates, toggleState, query } = useRunFilters()
  const { data, isLoading, error } = useCIRuns(owner, repo, "agent", query)

  return (
    <div className="repo">
      <section className="pipelines">
        <div className="pipelines__head">
          <h2 className="pipelines__title">Agents</h2>
        </div>
        <p className="muted small pipelines__lead">
          Agent runs are spawned from an issue (the “Spawn agent” button); each works the issue in
          an isolated container.
        </p>

        <RunFilters
          search={search}
          onSearch={setSearch}
          placeholder="Search commit, ref, or trigger…"
          activeStates={activeStates}
          onToggleState={toggleState}
        />

        {isLoading && <Spinner />}
        {error && <ErrorMessage error={error} />}
        {data && data.length === 0 && (
          <EmptyState>
            No agent runs match these filters. Open an issue and click “Spawn agent” to start one.
          </EmptyState>
        )}
        {data && data.length > 0 && (
          <AgentRunsTable showRepo={false} rows={data.map((run) => ({ owner, name: repo, run }))} />
        )}
      </section>
    </div>
  )
}

export default AgentsPage
