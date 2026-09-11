import { useMemo, useRef } from "react"
import { Link } from "react-router-dom"
import "./pipelines.css"
import { useVirtualizer } from "@tanstack/react-virtual"
import { useAllRuns } from "@/api/queries"
import RunFilters from "@/features/agents/RunFilters"
import { AGENT_CHIPS, CI_RUN_CHIPS } from "@/features/agents/runChips"
import { useRunFilters } from "@/features/agents/useRunFilters"
import CIStatusBadge from "@/features/pipelines/CIStatusBadge"
import {
  executionModelLabel,
  type RunKind,
  runDuration,
  runsBasePath,
  shortRef,
  shortSHA,
} from "@/features/pipelines/runHelpers"
import { repoHue } from "@/shell/repoColor"
import { absoluteTime, timeAgo } from "@/shell/timeAgo"
import { useRepoFilter } from "@/shell/useRepoFilter"
import { EmptyState, ErrorMessage, SkeletonTable, Table } from "@/ui"

// Fleet-wide runs view: every repo's CI runs (kind="ci", the Pipelines tab) or
// agent runs (kind="agent", the Agents tab) in one table, newest-created first.
// Each row links into the owning repo's per-repo run detail. Filterable by
// keyword, status, and repo.
export default function GlobalRunsPage({ kind }: { kind: RunKind }) {
  const isAgent = kind === "agent"
  const chips = isAgent ? AGENT_CHIPS : CI_RUN_CHIPS
  const { search, setSearch, activeStates, toggleState, query } = useRunFilters(chips)
  const { activeRepos, toggleRepo } = useRepoFilter()
  const { data, isLoading, error } = useAllRuns(kind, query)
  const base = runsBasePath(kind)

  const availableRepos = useMemo(
    () => [...new Set((data ?? []).map((r) => `${r.repo.owner}/${r.repo.name}`))].sort(),
    [data]
  )

  const rows = useMemo(() => {
    if (!data) return []
    if (activeRepos.length === 0) return data
    return data.filter((r) => activeRepos.includes(`${r.repo.owner}/${r.repo.name}`))
  }, [data, activeRepos])

  const placeholder = isAgent
    ? "Search commit, ref, or trigger across all repos…"
    : "Search commit, ref, or trigger across all repos…"

  const scrollRef = useRef<HTMLDivElement>(null)
  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => 40,
    overscan: 8,
  })
  const virtualItems = virtualizer.getVirtualItems()
  const totalSize = virtualizer.getTotalSize()
  const paddingTop = virtualItems.length > 0 ? (virtualItems[0]?.start ?? 0) : 0
  const paddingBottom =
    virtualItems.length > 0 ? totalSize - (virtualItems[virtualItems.length - 1]?.end ?? 0) : 0

  return (
    <section className="pipelines pipelines--list">
      <div className="pipelines__head">
        <h2 className="pipelines__title">{isAgent ? "Agents" : "Pipelines"}</h2>
      </div>
      <p className="muted small pipelines__lead">
        {isAgent
          ? "Every agent run across all repos. Open one to follow its transcript."
          : "Every pipeline run across all repos. Open one to see its job graph."}
      </p>

      <RunFilters
        chips={chips}
        search={search}
        onSearch={setSearch}
        placeholder={placeholder}
        activeStates={activeStates}
        onToggleState={toggleState}
        availableRepos={availableRepos}
        activeRepos={activeRepos}
        onToggleRepo={toggleRepo}
      />

      {isLoading && (
        <SkeletonTable
          className="ci-runs"
          headers={["Repo", "Run", "Status", "Commit", "Ref", "Trigger", "Duration", "When"]}
        />
      )}
      {error && <ErrorMessage error={error} />}
      {data && rows.length === 0 && (
        <EmptyState>
          {isAgent ? "No agent runs match these filters." : "No pipeline runs match these filters."}
        </EmptyState>
      )}

      {data && rows.length > 0 && (
        <div ref={scrollRef} className="pipelines__table-wrap">
          <Table className="ci-runs">
            <thead>
              <tr>
                <th>Repo</th>
                <th>Run</th>
                <th>Status</th>
                <th>Commit</th>
                <th>Ref</th>
                <th>Trigger</th>
                <th>Duration</th>
                <th>When</th>
              </tr>
            </thead>
            <tbody>
              {paddingTop > 0 && (
                <tr>
                  <td colSpan={8} style={{ height: paddingTop, padding: 0, border: 0 }} />
                </tr>
              )}
              {virtualItems.map((vrow) => {
                const run = rows[vrow.index]
                if (!run) return null
                const repoPath = `/${run.repo.owner}/${run.repo.name}`
                return (
                  <tr key={`${run.repo.owner}/${run.repo.name}#${run.number}`}>
                    <td>
                      <Link
                        className="repo-tag"
                        style={
                          {
                            "--repo-hue": repoHue(`${run.repo.owner}/${run.repo.name}`),
                          } as React.CSSProperties
                        }
                        to={repoPath}
                      >
                        {run.repo.owner}/{run.repo.name}
                      </Link>
                    </td>
                    <td>
                      <Link className="ci-runs__num" to={`${repoPath}/${base}/${run.number}`}>
                        #{run.number}
                      </Link>
                      {isAgent && (
                        <span className="ci-runs__model muted small">
                          {executionModelLabel(run.execution_model)}
                        </span>
                      )}
                    </td>
                    <td>
                      <CIStatusBadge status={run.status} />
                    </td>
                    <td className="ci-runs__commit">
                      <span className="ci-runs__msg" title={run.commit_msg || undefined}>
                        {run.commit_msg || "(no commit message)"}
                      </span>
                      <span className="ci-runs__sha muted small" title={run.commit_sha}>
                        {shortSHA(run.commit_sha)}
                        {run.commit_author ? ` · ${run.commit_author}` : ""}
                      </span>
                    </td>
                    <td className="muted">{shortRef(run.ref)}</td>
                    <td className="muted">{run.trigger || "—"}</td>
                    <td className="muted">{runDuration(run)}</td>
                    <td className="muted" title={absoluteTime(run.created_at)}>
                      {timeAgo(run.created_at)}
                    </td>
                  </tr>
                )
              })}
              {paddingBottom > 0 && (
                <tr>
                  <td colSpan={8} style={{ height: paddingBottom, padding: 0, border: 0 }} />
                </tr>
              )}
            </tbody>
          </Table>
        </div>
      )}
    </section>
  )
}
