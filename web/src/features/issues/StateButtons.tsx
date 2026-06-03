import clsx from "clsx"
import { useUpdateIssue } from "@/api/mutations"
import { ISSUE_STATES, type IssueState } from "@/api/types"
import StateIcon from "@/features/issues/StateIcon"

interface Props {
  owner: string
  repo: string
  number: number
  current: IssueState
}

/**
 * Vertical state picker for the issue sidebar. The active state shows
 * its own colored background; clicking another state PATCHes. Disabled
 * while the mutation is in flight or for the current value.
 */
export default function StateButtons({ owner, repo, number, current }: Props) {
  const mutation = useUpdateIssue(owner, repo, number)

  return (
    <div className="segmented">
      {ISSUE_STATES.map((s) => (
        <button
          key={s}
          type="button"
          className={clsx("segmented__btn", `segmented__btn--${s}`, { "is-active": s === current })}
          aria-current={s === current ? "true" : undefined}
          disabled={s === current || mutation.isPending}
          onClick={() => mutation.mutate({ state: s })}
        >
          <StateIcon state={s} size={14} />
          {s.replace("_", " ")}
        </button>
      ))}
    </div>
  )
}
