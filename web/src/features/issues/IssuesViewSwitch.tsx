import clsx from "clsx"
import { Link, useLocation, useParams } from "react-router-dom"

/**
 * Segmented switch between the list and board views of the same
 * issues data. The active view is derived from the URL, not a prop —
 * lets you bookmark either view directly.
 */
const IssuesViewSwitch = () => {
  const { owner = "", repo = "" } = useParams()
  const location = useLocation()
  const isBoard = location.pathname.endsWith("/board")

  const listPath = `/${owner}/${repo}/issues`
  const boardPath = `/${owner}/${repo}/issues/board`

  return (
    <div className="view-switch" role="tablist" aria-label="View mode">
      <Link
        to={listPath}
        role="tab"
        aria-selected={!isBoard}
        className={clsx("view-switch__btn", { "is-active": !isBoard })}
      >
        <svg width="14" height="14" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true">
          <path d="M2 4h12v1.5H2V4Zm0 3.25h12v1.5H2v-1.5Zm0 3.25h12V12H2v-1.5Z" />
        </svg>
        List
      </Link>
      <Link
        to={boardPath}
        role="tab"
        aria-selected={isBoard}
        className={clsx("view-switch__btn", { "is-active": isBoard })}
      >
        <svg width="14" height="14" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true">
          <path d="M2 3h3.5v10H2V3Zm4.5 0h3v6h-3V3Zm4 0H14v8h-3.5V3Z" />
        </svg>
        Board
      </Link>
    </div>
  )
}

export default IssuesViewSwitch
