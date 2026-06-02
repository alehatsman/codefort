import { lazy, Suspense, useState } from "react"
import { useParams } from "react-router-dom"
import { ApiError } from "../api/client"
import { useMergePull, useUpdatePull } from "../api/mutations"
import { usePull } from "../api/queries"
import type { MergeConflictResponse } from "../api/types"
import CompareView from "../components/CompareView"
import OverviewCard from "../components/OverviewCard"
import { Button, EmptyState, ErrorMessage, Spinner } from "../components/ui"

const Markdown = lazy(() => import("../components/Markdown"))

// conflictsFrom pulls the conflicting paths out of a 409 merge error body, if
// present (a content conflict carries them; other 409s don't).
function conflictsFrom(err: unknown): string[] {
  if (err instanceof ApiError && err.body) {
    const body = err.body as Partial<MergeConflictResponse>
    if (Array.isArray(body.conflicts)) return body.conflicts
  }
  return []
}

export default function PullPage() {
  const { owner = "", repo = "", number = "" } = useParams()
  const n = Number(number)

  const pullQ = usePull(owner, repo, n)
  const mergePull = useMergePull(owner, repo, n)
  const updatePull = useUpdatePull(owner, repo, n)
  const [ffOnly, setFFOnly] = useState(false)

  const pr = pullQ.data
  const conflicts = conflictsFrom(mergePull.error)
  const openComments = (pr?.comments ?? []).filter((c) => !c.resolved)

  // Mergeable only when open with commits to bring in (ahead > 0).
  const mergeable = pr?.state === "open" && pr.compare.ahead > 0

  return (
    <div className="pull-page">
      <OverviewCard owner={owner} repo={repo} path="" summaries={{}} />

      {pullQ.isLoading ? (
        <Spinner />
      ) : pullQ.error ? (
        <ErrorMessage error={pullQ.error} />
      ) : !pr ? (
        <EmptyState>Pull request not found.</EmptyState>
      ) : (
        <>
          <header className="pull-head">
            <h2 className="pull-head__title">
              {pr.title} <span className="pull-head__number">#{pr.number}</span>
            </h2>
            <div className="pull-head__meta muted small">
              <span className={`pr-state pr-state--${pr.state}`}>{pr.state}</span>
              <span className="pull-head__refs">
                {pr.head_ref} → {pr.base_ref}
              </span>
              {" · opened by "}
              {pr.author} on {new Date(pr.created_at).toLocaleDateString()}
              {pr.merged_at && <> · merged {new Date(pr.merged_at).toLocaleDateString()}</>}
            </div>
            {pr.body && (
              <div className="pull-head__body">
                <Suspense fallback={<div className="markdown-body loading">Loading…</div>}>
                  <Markdown content={pr.body} owner={owner} repo={repo} basePath="" />
                </Suspense>
              </div>
            )}
          </header>

          {pr.state === "open" && (
            <div className="pull-merge">
              <div className="pull-merge__actions">
                <Button
                  variant="primary"
                  disabled={!mergeable || mergePull.isPending}
                  onClick={() => mergePull.mutate({ method: ffOnly ? "ff-only" : "merge" })}
                >
                  {mergePull.isPending
                    ? "Merging…"
                    : ffOnly
                      ? "Fast-forward merge"
                      : "Merge pull request"}
                </Button>
                <label className="pull-merge__opt">
                  <input
                    type="checkbox"
                    checked={ffOnly}
                    onChange={(e) => setFFOnly(e.target.checked)}
                  />
                  fast-forward only
                </label>
                <Button
                  variant="danger"
                  disabled={updatePull.isPending}
                  onClick={() => updatePull.mutate({ state: "closed" })}
                >
                  Close
                </Button>
              </div>
              {!mergeable && pr.compare.ahead === 0 && (
                <EmptyState>Nothing to merge — head is already in base.</EmptyState>
              )}
              {conflicts.length > 0 ? (
                <div className="error inline">
                  Merge conflict — resolve locally and push, then retry. Conflicting files:
                  <ul className="pull-merge__conflicts">
                    {conflicts.map((p) => (
                      <li key={p}>{p}</li>
                    ))}
                  </ul>
                </div>
              ) : (
                mergePull.error && <ErrorMessage error={mergePull.error} inline />
              )}
            </div>
          )}

          {pr.state === "closed" && (
            <div className="pull-merge">
              <Button
                disabled={updatePull.isPending}
                onClick={() => updatePull.mutate({ state: "open" })}
              >
                Reopen
              </Button>
            </div>
          )}

          {openComments.length > 0 && (
            <div className="review-list">
              <h3>Review comments ({openComments.length})</h3>
              {openComments.map((c) => {
                const lines =
                  c.end_line > c.start_line ? `L${c.start_line}-L${c.end_line}` : `L${c.start_line}`
                return (
                  <div key={c.id} className="review-row">
                    <div className="review-row__head">
                      <span className="review-row__lines">
                        {c.path}:{lines}
                      </span>
                      <span className="muted small">@{c.author}</span>
                    </div>
                    <div className="review-row__body">{c.body}</div>
                    {c.snippet && <pre className="review-row__snippet">{c.snippet}</pre>}
                  </div>
                )
              })}
            </div>
          )}

          <CompareView compare={pr.compare} />
        </>
      )}
    </div>
  )
}
