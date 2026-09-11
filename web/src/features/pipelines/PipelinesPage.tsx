import { useRef, useState } from "react"
import "./pipelines.css"
import { useVirtualizer } from "@tanstack/react-virtual"
import clsx from "clsx"
import { Link, useNavigate, useParams } from "react-router-dom"
import { useCancelCIRun, useRerunCIRun, useSetCIEnabled, useTriggerCIRun } from "@/api/mutations"
import { useCIRun, useCIRuns, useRefs, useRepo } from "@/api/queries"
import type { CIRun, CIRunDetail, Repo } from "@/api/types"
import AgentRunBody from "@/features/agents/AgentRunBody"
import CIRunBody from "@/features/pipelines/CIRunBody"
import CIStatusBadge from "@/features/pipelines/CIStatusBadge"
import {
  executionModelLabel,
  isAgentRun,
  isRunActive,
  type RunKind,
  runDuration,
  runsBasePath,
  shortRef,
  shortSHA,
} from "@/features/pipelines/runHelpers"
import OverviewCard from "@/shell/OverviewCard"
import { absoluteTime, timeAgo } from "@/shell/timeAgo"
import {
  Button,
  EmptyState,
  ErrorMessage,
  Input,
  SkeletonTable,
  SkeletonText,
  Table,
  useToast,
} from "@/ui"

// Pipelines tab. One component serves the runs list (/pipelines) and a single
// run's detail (/pipelines/:number), distinguished by the URL — mirroring how
// RepoPage serves the code browser routes. The `kind` prop picks which tab:
// "ci" is Pipelines, "agent" is Agents. The run resource, list chrome, and run
// header are shared; the run *body* splits into CIRunBody (job DAG) vs
// AgentRunBody (transcript + message box).
export default function PipelinesPage({ kind = "ci" }: { kind?: RunKind }) {
  const { owner = "", repo = "" } = useParams()
  const numberParam = useParams()["number"]
  const repoQ = useRepo(owner, repo)

  if (repoQ.isLoading) return <SkeletonText lines={4} />
  if (repoQ.error) return <ErrorMessage error={repoQ.error} />
  if (!repoQ.data) return null

  const r = repoQ.data
  const runNumber = numberParam ? Number(numberParam) : null

  // The breadcrumb card sits above the run *list* (the tab landing). The run
  // *detail* view fills the viewport (.pipelines--agent-run is sized to
  // 100dvh - topbar - padding) and carries its own back-nav header, so a card
  // above it would push the full-height section past the viewport (#322).
  const isList = runNumber === null || !Number.isFinite(runNumber)
  return (
    <div className={clsx("repo", isList && "repo--list-view")}>
      {!isList ? (
        <RunDetail owner={r.owner} repo={r.name} runNumber={runNumber as number} />
      ) : (
        <>
          <OverviewCard owner={r.owner} repo={r.name} path="" summaries={{}} />
          <RunList repo={r} kind={kind} />
        </>
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
  const toast = useToast()
  // The input defaults to the repo's default branch until the user edits it
  // (null = untouched, so a freshly loaded default still flows through).
  const [refInput, setRefInput] = useState<string | null>(null)
  const ref = refInput ?? refsQ.data?.default ?? ""

  function runPipeline(e: React.FormEvent) {
    e.preventDefault()
    const r = ref.trim()
    if (!r) return
    trigger.mutate(r, {
      onSuccess: (run) => toast(`Pipeline run #${run.number} started`, { variant: "success" }),
    })
  }

  const isAgent = kind === "agent"
  const base = runsBasePath(kind)
  const runs = runsQ.data ?? []
  const colSpan = isAgent ? 6 : 7

  const scrollRef = useRef<HTMLDivElement>(null)
  const virtualizer = useVirtualizer({
    count: runs.length,
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
    <section className="pipelines">
      <RunListHead
        isAgent={isAgent}
        ref={ref}
        setRefInput={setRefInput}
        runPipeline={runPipeline}
        trigger={trigger}
        setEnabled={setEnabled}
      />
      <ErrorMessage error={trigger.error} inline />

      {runsQ.isLoading && (
        <SkeletonTable
          className="ci-runs"
          headers={
            isAgent
              ? ["Run", "Status", "Hash", "Trigger", "Duration", "When"]
              : ["Run", "Status", "Commit", "Ref", "Trigger", "Duration", "When"]
          }
        />
      )}
      <ErrorMessage error={runsQ.error} />
      {runsQ.data && runs.length === 0 && (
        <EmptyState>
          {isAgent ? (
            <>No agent runs yet. Open an issue and click "Spawn agent" to start one.</>
          ) : (
            <>
              No runs yet. Push a commit with an <code>mgitci.yml</code> to trigger the first one.
            </>
          )}
        </EmptyState>
      )}

      {runsQ.data && runs.length > 0 && (
        <div ref={scrollRef} className="pipelines__table-wrap">
          <Table className="ci-runs">
            <thead>
              <tr>
                <th>Run</th>
                <th>Status</th>
                <th>Commit</th>
                {!isAgent && <th>Ref</th>}
                <th>Trigger</th>
                <th>Duration</th>
                <th>When</th>
              </tr>
            </thead>
            <tbody>
              {paddingTop > 0 && (
                <tr>
                  <td colSpan={colSpan} style={{ height: paddingTop, padding: 0, border: 0 }} />
                </tr>
              )}
              {virtualItems.map((vrow) => {
                const run = runs[vrow.index]
                if (!run) return null
                return (
                  <RunRow
                    key={run.number}
                    run={run}
                    owner={owner}
                    repo={repo}
                    base={base}
                    isAgent={isAgent}
                  />
                )
              })}
              {paddingBottom > 0 && (
                <tr>
                  <td colSpan={colSpan} style={{ height: paddingBottom, padding: 0, border: 0 }} />
                </tr>
              )}
            </tbody>
          </Table>
        </div>
      )}
    </section>
  )
}

function RunListHead({
  isAgent,
  ref,
  setRefInput,
  runPipeline,
  trigger,
  setEnabled,
}: {
  isAgent: boolean
  ref: string
  setRefInput: (v: string) => void
  runPipeline: (e: React.FormEvent) => void
  trigger: ReturnType<typeof useTriggerCIRun>
  setEnabled: ReturnType<typeof useSetCIEnabled>
}) {
  return (
    <>
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
            Agent runs are spawned from an issue (the "Spawn agent" button); each works the issue in
            an isolated container.
          </>
        ) : (
          <>
            Runs trigger on push when an <code>mgitci.yml</code> is present at the pushed commit, or
            on demand for any ref above.
          </>
        )}
      </p>
    </>
  )
}

function RunRow({
  run,
  owner,
  repo,
  base,
  isAgent,
}: {
  run: CIRun
  owner: string
  repo: string
  base: string
  isAgent: boolean
}) {
  return (
    <tr>
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
      {!isAgent && <td className="muted">{shortRef(run.ref)}</td>}
      <td className="muted">{run.trigger || "—"}</td>
      <td className="muted">{runDuration(run)}</td>
      <td className="muted" title={absoluteTime(run.created_at)}>
        {timeAgo(run.created_at)}
      </td>
    </tr>
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

function RunDetail({ owner, repo, runNumber }: { owner: string; repo: string; runNumber: number }) {
  const navigate = useNavigate()
  const runQ = useCIRun(owner, repo, runNumber)
  const rerun = useRerunCIRun(owner, repo)
  const cancel = useCancelCIRun(owner, repo, runNumber)
  const toast = useToast()

  if (runQ.isLoading) return <SkeletonText lines={4} />
  if (runQ.error) return <ErrorMessage error={runQ.error} />
  if (!runQ.data) return null

  const run = runQ.data
  // Follow the run's *own* kind, not the route it was opened by: a spec-verify
  // run (#219) is an agent-family run (transcript body, Agents header) even via
  // a /pipelines/<n> link. One classifier (isAgentRun), no inline kind checks.
  const isAgent = isAgentRun(run.kind)
  const base = runsBasePath(isAgent ? "agent" : "ci")
  // An agent run carries its own Stop in AgentRunBody (it also offers Finish);
  // here we only stop CI runs, and only while they're still live.
  const canStopCI = !isAgent && isRunActive(run.status)

  function doRerun() {
    rerun.mutate(runNumber, {
      onSuccess: (created) => {
        toast(`Re-run started as #${created.number}`, { variant: "success" })
        void navigate(`/${owner}/${repo}/${base}/${created.number}`)
      },
    })
  }

  return (
    <section className={clsx("pipelines", { "pipelines--agent-run": isAgent })}>
      <RunDetailHead
        owner={owner}
        repo={repo}
        base={base}
        run={run}
        isAgent={isAgent}
        canStopCI={canStopCI}
        doRerun={doRerun}
        rerun={rerun}
        cancel={cancel}
        toast={toast}
      />
      <ErrorMessage error={rerun.error} inline />
      <ErrorMessage error={cancel.error} inline />

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
            <dd>{executionModelLabel(run.execution_model)}</dd>
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

      {isAgent ? (
        <AgentRunBody owner={owner} repo={repo} runNumber={runNumber} run={run} />
      ) : (
        <CIRunBody owner={owner} repo={repo} runNumber={runNumber} jobs={run.jobs} />
      )}
    </section>
  )
}

function RunDetailHead({
  owner,
  repo,
  base,
  run,
  isAgent,
  canStopCI,
  doRerun,
  rerun,
  cancel,
  toast,
}: {
  owner: string
  repo: string
  base: string
  run: CIRunDetail
  isAgent: boolean
  canStopCI: boolean
  doRerun: () => void
  rerun: ReturnType<typeof useRerunCIRun>
  cancel: ReturnType<typeof useCancelCIRun>
  toast: ReturnType<typeof useToast>
}) {
  return (
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
      {canStopCI && (
        <Button
          size="small"
          variant="danger"
          onClick={() =>
            cancel.mutate(undefined, {
              onSuccess: () => toast(`Run #${run.number} canceled`, { variant: "success" }),
            })
          }
          disabled={cancel.isPending}
          title="Force-stop the run now: interrupt its jobs and discard the workspace"
        >
          {cancel.isPending ? "Stopping…" : "Stop"}
        </Button>
      )}
    </div>
  )
}
