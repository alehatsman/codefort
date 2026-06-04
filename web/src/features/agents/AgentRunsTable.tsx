import { useRef } from "react"
import { Link } from "react-router-dom"
import { useVirtualizer } from "@tanstack/react-virtual"
import type { CIRun } from "@/api/types"
import CIStatusBadge from "@/features/pipelines/CIStatusBadge"
import { executionModelLabel, runDuration, shortSHA } from "@/features/pipelines/runHelpers"
import { repoHue } from "@/shell/repoColor"
import { absoluteTime, timeAgo } from "@/shell/timeAgo"
import { Table } from "@/ui"
import { runTrigger } from "@/features/agents/agentHelpers"

// AgentRunRow pairs a run with its owning repo so a single table serves both the
// per-repo Agents tab and the cross-repo /agents view (owner/name come from the
// route params or the run's repo respectively).
export interface AgentRunRow {
  owner: string
  name: string
  run: CIRun
}

// AgentRunsTable is the Agents-owned run grid: Repo (cross-repo only) · Run ·
// Status · Hash · Trigger · Duration · When. Virtual scroll via padding rows keeps
// the DOM lightweight for large fleets; the header is sticky inside the scroll area.
export default function AgentRunsTable({
  rows,
  showRepo,
}: {
  rows: AgentRunRow[]
  showRepo: boolean
}) {
  const colSpan = showRepo ? 7 : 6
  const scrollRef = useRef<HTMLDivElement>(null)
  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => 53,
    overscan: 8,
    measureElement: (el) => el.getBoundingClientRect().height,
  })

  const vItems = virtualizer.getVirtualItems()
  const paddingTop = vItems.length > 0 ? vItems[0].start : 0
  const paddingBottom =
    vItems.length > 0 ? virtualizer.getTotalSize() - vItems[vItems.length - 1].end : 0

  return (
    <div ref={scrollRef} className="agent-runs-scroll">
      <Table className="agent-runs">
        <thead>
          <tr>
            {showRepo && <th>Repo</th>}
            <th>Run</th>
            <th>Status</th>
            <th>Hash</th>
            <th>Trigger</th>
            <th>Duration</th>
            <th>When</th>
          </tr>
        </thead>
        <tbody>
          {paddingTop > 0 && (
            <tr>
              <td style={{ height: paddingTop, padding: 0, border: 0 }} colSpan={colSpan} />
            </tr>
          )}
          {vItems.map((vItem) => {
            const { owner, name, run } = rows[vItem.index]
            const repoPath = `/${owner}/${name}`
            const trigger = runTrigger(run)
            return (
              <tr
                key={`${owner}/${name}#${run.number}`}
                ref={virtualizer.measureElement}
                data-index={vItem.index}
              >
                {showRepo && (
                  <td>
                    <Link
                      className="repo-tag"
                      style={{ "--repo-hue": repoHue(`${owner}/${name}`) } as React.CSSProperties}
                      to={repoPath}
                    >
                      {owner}/{name}
                    </Link>
                  </td>
                )}
                <td>
                  <Link className="agent-runs__num" to={`${repoPath}/agents/${run.number}`}>
                    #{run.number}
                  </Link>
                  <span className="agent-runs__model muted small">
                    {executionModelLabel(run.execution_model)}
                  </span>
                </td>
                <td>
                  <CIStatusBadge status={run.status} />
                </td>
                <td>
                  {run.commit_sha ? (
                    <Link
                      className="agent-runs__sha"
                      to={`${repoPath}/commit/${run.commit_sha}`}
                      title={run.commit_sha}
                    >
                      {shortSHA(run.commit_sha)}
                    </Link>
                  ) : (
                    <span className="muted">—</span>
                  )}
                </td>
                <td>
                  {trigger.issue !== undefined ? (
                    <Link to={`${repoPath}/issues/${trigger.issue}`}>{trigger.label}</Link>
                  ) : (
                    <span className="muted">{trigger.label}</span>
                  )}
                </td>
                <td className="muted">{runDuration(run)}</td>
                <td className="muted" title={absoluteTime(run.created_at)}>
                  {timeAgo(run.created_at)}
                </td>
              </tr>
            )
          })}
          {paddingBottom > 0 && (
            <tr>
              <td style={{ height: paddingBottom, padding: 0, border: 0 }} colSpan={colSpan} />
            </tr>
          )}
        </tbody>
      </Table>
    </div>
  )
}
