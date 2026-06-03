import { useUpdateIssue } from "@/api/mutations"
import { ISSUE_STATES, type IssueState } from "@/api/types"
import StateIcon from "@/features/issues/StateIcon"
import { SegmentedControl } from "@/ui"

interface Props {
  owner: string
  repo: string
  number: number
  current: IssueState
}

/**
 * Vertical state picker for the issue sidebar. The active state shows its own
 * colored background; clicking another state PATCHes. Disabled while the
 * mutation is in flight or for the current value (lockActive).
 */
export default function StateButtons({ owner, repo, number, current }: Props) {
  const mutation = useUpdateIssue(owner, repo, number)

  return (
    <SegmentedControl
      label="Issue state"
      value={current}
      onChange={(state) => mutation.mutate({ state })}
      disabled={mutation.isPending}
      lockActive
      options={ISSUE_STATES.map((s) => ({
        value: s,
        label: s.replace("_", " "),
        icon: <StateIcon state={s} size={14} />,
        className: `segmented__btn--${s}`,
      }))}
    />
  )
}
