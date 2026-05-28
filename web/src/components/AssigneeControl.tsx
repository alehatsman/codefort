import { useClaimIssue, useUnclaimIssue } from "../api/mutations"
import Avatar from "./Avatar"

interface Props {
  owner: string
  repo: string
  number: number
  assignee: string | null
  /** Current user's name (from useWhoami), used to show "(you)". */
  me?: string
}

/**
 * Sidebar control for assignment. Shows the assignee + a contextual
 * action (Claim if unassigned, Release if assigned). Anyone with write
 * access can release in this slice.
 */
export default function AssigneeControl({ owner, repo, number, assignee, me }: Props) {
  const claim = useClaimIssue(owner, repo, number)
  const unclaim = useUnclaimIssue(owner, repo, number)
  const inFlight = claim.isPending || unclaim.isPending
  const error = claim.error || unclaim.error

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
      <div className="assignee__row">
        {assignee === null ? (
          <button className="btn btn--small" disabled={inFlight} onClick={() => claim.mutate({})}>
            {claim.isPending ? "Claiming…" : "Claim it"}
          </button>
        ) : (
          <button className="btn btn--small" disabled={inFlight} onClick={() => unclaim.mutate()}>
            {unclaim.isPending ? "Releasing…" : "Release"}
          </button>
        )}
      </div>
      {error && <div className="error inline">{(error as Error).message}</div>}
    </div>
  )
}
