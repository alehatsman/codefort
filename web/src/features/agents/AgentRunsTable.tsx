import { Link } from "react-router-dom"
import type { CIRun } from "@/api/types"
import { Table } from "@/ui"
import CIStatusBadge from "@/features/pipelines/CIStatusBadge"
import { executionModelLabel, runDuration, shortSHA } from "@/features/pipelines/runHelpers"
import { absoluteTime, timeAgo } from "@/shell/timeAgo"
import { runTrigger } from "@/features/agents/agentHelpers"

// AgentRunRow pairs a run with its owning repo so a single table serves both the
// per-repo Agents tab and the cross-repo /agents view (owner/name come from the
// route params or the run's repo respectively).
export interface AgentRunRow {
  owner: string
  name: string
  run: CIRun
}

// AgentRunsTable is the Agents-owned run grid (replacing the shared pipelines
// table for agents): Repo (cross-repo only) · Run · Status · Hash · Trigger ·
// Duration · When. Trigger surfaces the spawning issue/review, linked to it.
export default function AgentRunsTable({
  rows,
  showRepo,
}: {
  rows: AgentRunRow[]
  showRepo: boolean
}) {
  return (
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
        {rows.map(({ owner, name, run }) => {
          const repoPath = `/${owner}/${name}`
          const trigger = runTrigger(run)
          return (
            <tr key={`${owner}/${name}#${run.number}`}>
              {showRepo && (
                <td>
                  <Link className="agent-runs__repo" to={repoPath}>
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
      </tbody>
    </Table>
  )
}
