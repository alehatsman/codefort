import { lazy, Suspense } from "react"
import "./pulls.css"
import { useParams } from "react-router-dom"
import { ApiError } from "@/api/client"
import { useCreateCodeComment, useMergePull, useUpdatePull } from "@/api/mutations"
import { useCommitCIStatus, usePull } from "@/api/queries"
import type { MergeConflictResponse } from "@/api/types"
import CommitCIStatus from "@/features/commits/CommitCIStatus"
import CompareView from "@/features/pulls/CompareView"
import OverviewCard from "@/shell/OverviewCard"
import { Button, DetailLayout, EmptyState, ErrorMessage, SkeletonText, useToast } from "@/ui"

const Markdown = lazy(() => import("@/shell/Markdown"))

// conflictsFrom pulls the conflicting paths out of a 409 merge error body, if
// present (a content conflict carries them; other 409s don't).
function conflictsFrom(err: unknown): string[] {
  if (err instanceof ApiError && err.body) {
    const body = err.body as Partial<MergeConflictResponse>
    if (Array.isArray(body.conflicts)) return body.conflicts
  }
  return []
}

// isNotFastForwardable detects the 409 returned when ff-only fails but there
// are no content conflicts — the branches have diverged.
function isNotFastForwardable(err: unknown): boolean {
  return err instanceof ApiError && err.status === 409 && conflictsFrom(err).length === 0
}

export default function PullPage() {
  const { owner = "", repo = "", number = "" } = useParams()
  const n = Number(number)

  const pullQ = usePull(owner, repo, n)
  const mergePull = useMergePull(owner, repo, n)
  const updatePull = useUpdatePull(owner, repo, n)
  const createComment = useCreateCodeComment(owner, repo)
  const ciStatusQ = useCommitCIStatus(owner, repo)
  const toast = useToast()
  const pr = pullQ.data
  const headRun = pr ? ciStatusQ.data?.get(pr.compare.head) : undefined
  const conflicts = conflictsFrom(mergePull.error)
  const notFastForwardable = isNotFastForwardable(mergePull.error)

  // Mergeable only when open with commits to bring in (ahead > 0).
  const mergeable = pr?.state === "open" && pr.compare.ahead > 0

  async function handleAddComment(path: string, line: number, body: string) {
    if (!pr) return
    await createComment.mutateAsync({
      ref: pr.head_ref,
      path,
      start_line: line,
      end_line: line,
      body,
    })
  }

  return (
    <div className="pull-page">
      <OverviewCard owner={owner} repo={repo} path="" summaries={{}} />

      {pullQ.isLoading ? (
        <SkeletonText lines={4} />
      ) : pullQ.error ? (
        <ErrorMessage error={pullQ.error} />
      ) : !pr ? (
        <EmptyState>Pull request not found.</EmptyState>
      ) : (
        <DetailLayout>
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
          {headRun && <CommitCIStatus owner={owner} repo={repo} run={headRun} />}
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
                {headRun && <CommitCIStatus owner={owner} repo={repo} run={headRun} />}
                <Button
                  variant="primary"
                  disabled={!mergeable || mergePull.isPending}
                  onClick={() =>
                    mergePull.mutate(
                      { method: "ff-only" },
                      {
                        onSuccess: () => toast(`Pull request #${n} merged`, { variant: "success" }),
                      }
                    )
                  }
                >
                  {mergePull.isPending ? "Merging…" : "Merge pull request"}
                </Button>
                {notFastForwardable && (
                  <Button
                    disabled={mergePull.isPending}
                    onClick={() =>
                      mergePull.mutate(
                        { method: "merge" },
                        {
                          onSuccess: () =>
                            toast(`Pull request #${n} merged`, { variant: "success" }),
                        }
                      )
                    }
                  >
                    Merge with commit
                  </Button>
                )}
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
              ) : notFastForwardable ? (
                <div className="pull-merge__warn">
                  Branches have diverged — fast-forward not possible. Use "Merge with commit" to
                  create a merge commit, or rebase the head branch onto base and push.
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

          <CompareView
            compare={pr.compare}
            comments={pr.comments}
            onAddComment={handleAddComment}
          />
        </DetailLayout>
      )}
    </div>
  )
}
