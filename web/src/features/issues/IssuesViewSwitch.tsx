import clsx from "clsx"
import { Link, useLocation, useParams } from "react-router-dom"

/**
 * Segmented switch between the board and list views of the same issues data.
 * Works at both levels: per-repo (`/:owner/:repo/issues…`) and the fleet-wide
 * global view (`/issues…`) when no owner/repo is in the route. Board sits on
 * the left and is the default — the active view is derived from the URL, so
 * either view stays bookmarkable.
 */
export default function IssuesViewSwitch() {
  const { owner = "", repo = "" } = useParams()
  const location = useLocation()
  const isBoard = location.pathname.endsWith("/board")

  // Per-repo routes are scoped under /:owner/:repo; the global view is bare.
  const base = owner && repo ? `/${owner}/${repo}/issues` : "/issues"
  const listPath = base
  const boardPath = `${base}/board`

  return (
    <div className="view-switch" role="tablist" aria-label="View mode">
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
    </div>
  )
}
