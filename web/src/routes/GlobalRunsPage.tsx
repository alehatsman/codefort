import { Link } from "react-router-dom"
import { useAllRuns } from "../api/queries"
import CIStatusBadge from "../components/CIStatusBadge"
import { EmptyState, Spinner } from "../components/ui"
import { absoluteTime, timeAgo } from "../lib/timeAgo"
import {
  type RunKind,
  executionModelLabel,
  runDuration,
  runsBasePath,
  shortRef,
  shortSHA,
} from "./runHelpers"

// Fleet-wide runs view: every repo's CI runs (kind="ci", the Pipelines tab) or
// agent runs (kind="agent", the Agents tab) in one table, newest-created first.
// Each row links into the owning repo's per-repo run detail (which carries the
// job DAG / agent transcript). List-only — the detail routes stay repo-scoped.
export default function GlobalRunsPage({ kind }: { kind: RunKind }) {
  const { data, isLoading, error } = useAllRuns(kind)
  const isAgent = kind === "agent"
  const base = runsBasePath(kind)

  return (
    <section className="pipelines">
      <div className="pipelines__head">
        <h2 className="pipelines__title">{isAgent ? "Agents" : "Pipelines"}</h2>
      </div>
      <p className="muted small pipelines__lead">
        {isAgent
          ? "Every agent run across all repos. Open one to follow its transcript."
          : "Every pipeline run across all repos. Open one to see its job graph."}
      </p>

      {isLoading && <Spinner />}
      {error && <div className="error">{(error as Error).message}</div>}
      {data && data.length === 0 && (
        <EmptyState>{isAgent ? "No agent runs yet." : "No pipeline runs yet."}</EmptyState>
      )}

      {data && data.length > 0 && (
        <table className="ci-runs">
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
            {data.map((run) => {
              const repoPath = `/${run.repo.owner}/${run.repo.name}`
              return (
                <tr key={`${run.repo.owner}/${run.repo.name}#${run.number}`}>
                  <td>
                    <Link className="ci-runs__repo" to={repoPath}>
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
          </tbody>
        </table>
      )}
    </section>
  )
}
