import { useDroppable } from "@dnd-kit/core"
import clsx from "clsx"
import type { Issue, IssueState } from "@/api/types"
import BoardCard from "@/features/issues/BoardCard"
import StateIcon from "@/features/issues/StateIcon"

interface Props {
  owner: string
  repo: string
  state: IssueState
  issues: Issue[]
}

/**
 * Drop target for one state. The header shows state name + count;
 * the body lists draggable cards. Visual highlight when something
 * is hovering over it.
 */
export default function BoardColumn({ owner, repo, state, issues }: Props) {
  const { setNodeRef, isOver } = useDroppable({
    id: `column-${state}`,
    data: { state },
  })

  return (
    <div
      ref={setNodeRef}
      className={clsx("board-col", { "is-over": isOver })}
      data-testid={`board-column-${state}`}
    >
      <div className={`board-col__head board-col__head--${state}`}>
        <StateIcon state={state} size={14} />
        <span className="board-col__name">{state.replace("_", " ")}</span>
        <span className="board-col__count">{issues.length}</span>
      </div>
      <div className="board-col__body">
        {issues.length === 0 && <div className="board-col__empty muted">No issues</div>}
        {issues.map((iss) => (
          <BoardCard key={iss.id} owner={owner} repo={repo} issue={iss} />
        ))}
      </div>
    </div>
  )
}
