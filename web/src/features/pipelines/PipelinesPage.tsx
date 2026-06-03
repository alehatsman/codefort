import { useState } from "react"
import "./pipelines.css"
import { Link, useNavigate, useParams } from "react-router-dom"
import { useCIRun, useCIRuns, useRefs, useRepo } from "@/api/queries"
import { useRerunCIRun, useSetCIEnabled, useTriggerCIRun } from "@/api/mutations"
import { absoluteTime, timeAgo } from "@/shell/timeAgo"
import type { Repo } from "@/api/types"
import CIStatusBadge from "@/features/pipelines/CIStatusBadge"
import { Button, EmptyState, ErrorMessage, Input, Spinner } from "@/ui"
import AgentRunBody from "@/features/agents/AgentRunBody"
import CIRunBody from "@/features/pipelines/CIRunBody"
import {
  type RunKind,
  executionModelLabel,
  runDuration,
  runsBasePath,
  shortRef,
  shortSHA,
} from "@/features/pipelines/runHelpers"

// Pipelines tab. One component serves the runs list (/pipelines) and a single
// run's detail (/pipelines/:number), distinguished by the URL — mirroring how
// RepoPage serves the code browser routes. The `kind` prop picks which tab:
// "ci" is Pipelines, "agent" is Agents. The run resource, list chrome, and run
// header are shared; the run *body* splits into CIRunBody (job DAG) vs
// AgentRunBody (transcript + message box).
export default function PipelinesPage({ kind = "ci" }: { kind?: RunKind }) {
  const { owner = "", repo = "" } = useParams()
  const numberParam = useParams().number
  const repoQ = useRepo(owner, repo)

  if (repoQ.isLoading) return <Spinner />
  if (repoQ.error) return <ErrorMessage error={repoQ.error} />
  if (!repoQ.data) return null

  const r = repoQ.data
  const runNumber = numberParam ? Number(numberParam) : null

  return (
    <div className="repo">
      {runNumber !== null && Number.isFinite(runNumber) ? (
        <RunDetail owner={r.owner} repo={r.name} runNumber={runNumber} kind={kind} />
      ) : (
        <RunList repo={r} kind={kind} />
      )}
    </div>
  )
}

function RunList({ repo, kind }: { repo: Repo; kind: RunKind }) {
  const { owner, name } = repo
  // Agent runs are spawned from issues regardless of the CI opt-in, so the
  // Agents tab never shows the CI-disabled gate.
  if (kind === "ci" && !repo.ci_enabled) return <CIDisabledCard owner={owner} repo={name} />

  return <EnabledRunList owner={owner} repo={name} kind={kind} />
}

function EnabledRunList({ owner, repo, kind }: { owner: string; repo: string; kind: RunKind }) {
  const runsQ = useCIRuns(owner, repo, kind)
  const setEnabled = useSetCIEnabled(owner, repo)
  const refsQ = useRefs(owner, repo)
  const trigger = useTriggerCIRun(owner, repo)
  // The input defaults to the repo's default branch until the user edits it
  // (null = untouched, so a freshly loaded default still flows through).
  const [refInput, setRefInput] = useState<string | null>(null)
  const ref = refInput ?? refsQ.data?.default ?? ""

  function runPipeline(e: React.FormEvent) {
    e.preventDefault()
    const r = ref.trim()
    if (!r) return
    trigger.mutate(r)
  }

  const isAgent = kind === "agent"
  const base = runsBasePath(kind)

  return (
    <section className="pipelines">
      <div className="pipelines__head">
        <h2 className="pipelines__title">{isAgent ? "Agents" : "Pipelines"}</h2>
        {!isAgent && (
          <div className="pipelines__actions">
            <form className="pipelines__run" onSubmit={runPipeline}>
              <Input
                className="pipelines__run-ref"
                value={ref}
                onChange={(e) => setRefInput(e.target.value)}
                placeholder="branch, tag, or commit"
                aria-label="Ref to run"
              />
              <Button
                type="submit"
                variant="primary"
                size="small"
                disabled={trigger.isPending || ref.trim() === ""}
              >
                {trigger.isPending ? "Running…" : "Run pipeline"}
              </Button>
            </form>
            <Button
              size="small"
              onClick={() => setEnabled.mutate(false)}
              disabled={setEnabled.isPending}
              title="Disable CI for this repo"
            >
              Disable CI
            </Button>
          </div>
        )}
      </div>
      <p className="muted small pipelines__lead">
        {isAgent ? (
          <>
            Agent runs are spawned from an issue (the “Spawn agent” button); each works the issue in
            an isolated container.
          </>
        ) : (
          <>
            Runs trigger on push when an <code>mgitci.yml</code> is present at the pushed commit, or
            on demand for any ref above.
          </>
        )}
      </p>
      <ErrorMessage error={trigger.error} inline />

      {runsQ.isLoading && <Spinner />}
      <ErrorMessage error={runsQ.error} />
      {runsQ.data && runsQ.data.length === 0 && (
        <EmptyState>
          {isAgent ? (
            <>No agent runs yet. Open an issue and click “Spawn agent” to start one.</>
          ) : (
            <>
              No runs yet. Push a commit with an <code>mgitci.yml</code> to trigger the first one.
            </>
          )}
        </EmptyState>
      )}

      {runsQ.data && runsQ.data.length > 0 && (
        <table className="ci-runs">
          <thead>
            <tr>
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
            {runsQ.data.map((run) => (
              <tr key={run.number}>
                <td>
                  <Link className="ci-runs__num" to={`/${owner}/${repo}/${base}/${run.number}`}>
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
            ))}
          </tbody>
        </table>
      )}
    </section>
  )
}

function CIDisabledCard({ owner, repo }: { owner: string; repo: string }) {
  const setEnabled = useSetCIEnabled(owner, repo)
  return (
    <section className="pipelines">
      <h2 className="pipelines__title">Pipelines</h2>
      <EmptyState>
        <p>
          <strong>CI is disabled for this repository.</strong>
        </p>
        <p className="muted small">
          When enabled, pushing a commit that contains an <code>mgitci.yml</code> runs its pipeline.
          Each job runs in a throwaway container off the configured CI image, so repo-authored
          commands stay isolated from the host.
        </p>
        <Button
          variant="primary"
          onClick={() => setEnabled.mutate(true)}
          disabled={setEnabled.isPending}
        >
          {setEnabled.isPending ? "Enabling…" : "Enable CI"}
        </Button>
        <ErrorMessage error={setEnabled.error} inline />
      </EmptyState>
    </section>
  )
}

function RunDetail({
  owner,
  repo,
  runNumber,
  kind,
}: {
  owner: string
  repo: string
  runNumber: number
  kind: RunKind
}) {
  const navigate = useNavigate()
  const runQ = useCIRun(owner, repo, runNumber)
  const rerun = useRerunCIRun(owner, repo)

  if (runQ.isLoading) return <Spinner />
  if (runQ.error) return <ErrorMessage error={runQ.error} />
  if (!runQ.data) return null

  const run = runQ.data
  const base = runsBasePath(kind)
  const isAgent = kind === "agent"

  function doRerun() {
    rerun.mutate(runNumber, {
      onSuccess: (created) => navigate(`/${owner}/${repo}/${base}/${created.number}`),
    })
  }

  return (
    <section className="pipelines">
      <div className="ci-run-head">
        <Link className="ci-run-head__back" to={`/${owner}/${repo}/${base}`}>
          ← {isAgent ? "Agents" : "Pipelines"}
        </Link>
        <h2 className="pipelines__title">
          Run #{run.number} <CIStatusBadge status={run.status} />
        </h2>
        {/* Re-run re-enqueues a CI run, which is meaningless for an agent run. */}
        {!isAgent && (
          <Button size="small" onClick={doRerun} disabled={rerun.isPending}>
            {rerun.isPending ? "Re-running…" : "Re-run"}
          </Button>
        )}
      </div>
      <ErrorMessage error={rerun.error} inline />

      {run.commit_msg && <p className="ci-run-subject">{run.commit_msg}</p>}

      <dl className="ci-run-meta">
        <div>
          <dt>Commit</dt>
          <dd className="ci-runs__sha" title={run.commit_sha}>
            {run.commit_sha ? (
              <Link to={`/${owner}/${repo}/commit/${run.commit_sha}`}>
                {shortSHA(run.commit_sha)}
              </Link>
            ) : (
              shortSHA(run.commit_sha)
            )}
            {run.commit_author ? ` · ${run.commit_author}` : ""}
          </dd>
        </div>
        <div>
          <dt>Ref</dt>
          <dd>{shortRef(run.ref)}</dd>
        </div>
        {isAgent && (
          <div>
            <dt>Model</dt>
            <dd>
              {executionModelLabel(run.execution_model)}
              {run.execution_model === "mooncake-agent" && (
                <span className="muted small">
                  {" "}
                  · shell {run.mooncake_allow_shell ? "allowed" : "denied"}
                </span>
              )}
            </dd>
          </div>
        )}
        <div>
          <dt>Trigger</dt>
          <dd>{run.trigger || "—"}</dd>
        </div>
        <div>
          <dt>Duration</dt>
          <dd>{runDuration(run)}</dd>
        </div>
        <div>
          <dt>Started</dt>
          <dd title={run.started_at ? absoluteTime(run.started_at) : ""}>
            {run.started_at ? timeAgo(run.started_at) : "—"}
          </dd>
        </div>
      </dl>

      {run.kind === "agent" ? (
        <AgentRunBody owner={owner} repo={repo} runNumber={runNumber} run={run} />
      ) : (
        <CIRunBody owner={owner} repo={repo} runNumber={runNumber} jobs={run.jobs} />
      )}
    </section>
  )
}
