import { useMemo } from "react"
import "./agents.css"
import { useAllRuns } from "@/api/queries"
import AgentRunsTable from "@/features/agents/AgentRunsTable"
import RunFilters from "@/features/agents/RunFilters"
import { useRunFilters } from "@/features/agents/useRunFilters"
import { useRepoFilter } from "@/shell/useRepoFilter"
import { EmptyState, ErrorMessage, SkeletonTable } from "@/ui"

// Fleet-wide Agents view: every repo's agent runs in one grid, newest-created
// first, filterable by status, keyword, and repo. Each row links into the
// owning repo's agent run detail.
export default function GlobalAgentsPage() {
  const { search, setSearch, activeStates, toggleState, query } = useRunFilters()
  const { activeRepos, toggleRepo } = useRepoFilter()
  const { data, isLoading, error } = useAllRuns("agent", query)

  const availableRepos = useMemo(
    () => [...new Set((data ?? []).map((r) => `${r.repo.owner}/${r.repo.name}`))].sort(),
    [data]
  )

  const rows = useMemo(() => {
    if (!data) return []
    if (activeRepos.length === 0) return data
    return data.filter((r) => activeRepos.includes(`${r.repo.owner}/${r.repo.name}`))
  }, [data, activeRepos])

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
        availableRepos={availableRepos}
        activeRepos={activeRepos}
        onToggleRepo={toggleRepo}
      />

      {isLoading && (
        <SkeletonTable
          className="agent-runs"
          headers={["Repo", "Run", "Status", "Hash", "Trigger", "Duration", "When"]}
        />
      )}
      {error && <ErrorMessage error={error} />}
      {data && rows.length === 0 && <EmptyState>No agent runs match these filters.</EmptyState>}
      {data && rows.length > 0 && (
        <AgentRunsTable
          showRepo
          rows={rows.map((run) => ({ owner: run.repo.owner, name: run.repo.name, run }))}
        />
      )}
    </section>
  )
}
