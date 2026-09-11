import { useClaimIssue, useUnclaimIssue } from "@/api/mutations"
import type { IssueState } from "@/api/types"
import { Avatar, Button, ErrorMessage, useToast } from "@/ui"

interface Props {
  owner: string
  repo: string
  number: number
  assignee: string | null
  /** Issue lifecycle state — terminal states show attribution, not a live claim. */
  state: IssueState
  /** Current user's name (from useWhoami), used to show "(you)". */
  me?: string | undefined
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
  const toast = useToast()
  const inFlight = claim.isPending || unclaim.isPending
  const error = claim.error || unclaim.error
  const terminal = state === "done" || state === "closed"

  return (
    <div className="assignee">
      <div className="assignee__row">
        <AssigneeName assignee={assignee} me={me} />
      </div>
      {terminal ? (
        assignee !== null && <div className="muted small">Completed by {assignee}</div>
      ) : (
        <div className="assignee__row">
          <AssigneeAction
            assignee={assignee}
            number={number}
            inFlight={inFlight}
            claim={claim}
            unclaim={unclaim}
            toast={toast}
          />
        </div>
      )}
      {error && <ErrorMessage error={error} inline />}
    </div>
  )
}

function AssigneeName({ assignee, me }: { assignee: string | null; me?: string | undefined }) {
  if (assignee === null) return <span className="muted">No one assigned</span>
  return (
    <>
      <Avatar name={assignee} />
      <strong>{assignee}</strong>
      {me && assignee === me && <span className="muted small">(you)</span>}
    </>
  )
}

function AssigneeAction({
  assignee,
  number,
  inFlight,
  claim,
  unclaim,
  toast,
}: {
  assignee: string | null
  number: number
  inFlight: boolean
  claim: ReturnType<typeof useClaimIssue>
  unclaim: ReturnType<typeof useUnclaimIssue>
  toast: ReturnType<typeof useToast>
}) {
  if (assignee === null) {
    return (
      <Button
        size="small"
        disabled={inFlight}
        onClick={() =>
          claim.mutate({}, { onSuccess: () => toast(`Claimed #${number}`, { variant: "success" }) })
        }
      >
        {claim.isPending ? "Claiming…" : "Claim it"}
      </Button>
    )
  }
  return (
    <Button
      size="small"
      disabled={inFlight}
      onClick={() =>
        unclaim.mutate(undefined, {
          onSuccess: () => toast(`Released #${number}`, { variant: "success" }),
        })
      }
    >
      {unclaim.isPending ? "Releasing…" : "Release"}
    </Button>
  )
}
