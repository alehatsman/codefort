import { useClaimIssue, useUnclaimIssue } from "@/api/mutations"
import type { IssueState } from "@/api/types"
import Avatar from "@/shell/Avatar"
import { Button, ErrorMessage } from "@/ui"

interface Props {
  owner: string
  repo: string
  number: number
  assignee: string | null
  /** Issue lifecycle state — terminal states show attribution, not a live claim. */
  state: IssueState
  /** Current user's name (from useWhoami), used to show "(you)". */
  me?: string
}

/**
 * Sidebar control for assignment. Shows the assignee + a contextual
 * action (Claim if unassigned, Release if assigned). Anyone with write
 * access can release in this slice. On terminal states (done/closed) the
 * assignee is completion attribution, not a live lease, so the claim/release
 * action is hidden — it reads "Completed by" instead.
 */
export default function AssigneeControl({ owner, repo, number, assignee, state, me }: Props) {
  const claim = useClaimIssue(owner, repo, number)
  const unclaim = useUnclaimIssue(owner, repo, number)
  const inFlight = claim.isPending || unclaim.isPending
  const error = claim.error || unclaim.error
  const terminal = state === "done" || state === "closed"

  return (
    <div className="assignee">
      <div className="assignee__row">
        {assignee === null ? (
          <span className="muted">No one assigned</span>
        ) : (
          <>
            <Avatar name={assignee} />
            <strong>{assignee}</strong>
            {me && assignee === me && <span className="muted small">(you)</span>}
          </>
        )}
      </div>
      {terminal ? (
        assignee !== null && <div className="muted small">Completed by {assignee}</div>
      ) : (
        <div className="assignee__row">
          {assignee === null ? (
            <Button size="small" disabled={inFlight} onClick={() => claim.mutate({})}>
              {claim.isPending ? "Claiming…" : "Claim it"}
            </Button>
          ) : (
            <Button size="small" disabled={inFlight} onClick={() => unclaim.mutate()}>
              {unclaim.isPending ? "Releasing…" : "Release"}
            </Button>
          )}
        </div>
      )}
      {error && <ErrorMessage error={error} inline />}
    </div>
  )
}
