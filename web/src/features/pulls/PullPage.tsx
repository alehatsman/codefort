import { lazy, Suspense } from "react"
import "./pulls.css"
import { useParams } from "react-router-dom"
import { ApiError } from "@/api/client"
import { useCreateCodeComment, useMergePull, useSubmitReview, useUpdatePull } from "@/api/mutations"
import { usePull, useWhoami } from "@/api/queries"
import type { MergeConflictResponse, PRReview } from "@/api/types"
import CompareView from "@/features/pulls/CompareView"
import OverviewCard from "@/shell/OverviewCard"
import { Avatar, Badge, Button, DetailLayout, EmptyState, ErrorMessage, SidebarSection, SkeletonText, useToast } from "@/ui"

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
  const submitReview = useSubmitReview(owner, repo, n)
  const whoami = useWhoami()
  const toast = useToast()
  const pr = pullQ.data
  const myReview = pr?.reviews?.find((rv) => rv.author === whoami.data?.name)
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
        <DetailLayout
          sidebar={
            pr ? (
              <ReviewSidebar
                reviews={pr.reviews ?? []}
                myReview={myReview}
                isOpen={pr.state === "open"}
                onReview={(state) =>
                  submitReview.mutate(state, {
                    onSuccess: () => toast(state === "approved" ? "Approved" : "Changes requested"),
                  })
                }
                isPending={submitReview.isPending}
              />
            ) : undefined
          }
        >
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

function ReviewSidebar({
  reviews,
  myReview,
  isOpen,
  onReview,
  isPending,
}: {
  reviews: PRReview[]
  myReview?: PRReview
  isOpen: boolean
  onReview: (state: import("@/api/types").PRReviewState) => void
  isPending: boolean
}) {
  return (
    <SidebarSection label="Reviewers">
      {reviews.length === 0 && <span className="muted small">No reviews yet</span>}
      <ul className="review-list review-list--compact">
        {reviews.map((rv) => (
          <li key={rv.id} className="review-row review-row--compact">
            <Avatar name={rv.author} />
            <span className="review-row__author">{rv.author}</span>
            <Badge state={rv.state === "approved" ? "done" : "in_progress"}>
              {rv.state === "approved" ? "approved" : "changes requested"}
            </Badge>
          </li>
        ))}
      </ul>
      {isOpen && (
        <div className="review-actions">
          <Button
            variant={myReview?.state === "approved" ? "primary" : "ghost"}
            size="small"
            disabled={isPending}
            onClick={() => onReview("approved")}
          >
            {myReview?.state === "approved" ? "✓ Approved" : "Approve"}
          </Button>
          <Button
            variant={myReview?.state === "changes_requested" ? "danger" : "ghost"}
            size="small"
            disabled={isPending}
            onClick={() => onReview("changes_requested")}
          >
            {myReview?.state === "changes_requested" ? "✗ Changes requested" : "Request changes"}
          </Button>
        </div>
      )}
    </SidebarSection>
  )
}
