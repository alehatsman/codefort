import { Link, useLocation } from "react-router-dom"
import { clearToken } from "../api/client"
import { useRepo } from "../api/queries"
import RepoTabs from "./RepoTabs"

interface Props {
  children: React.ReactNode
  onSignOut: () => void
}

// Derive the repo context from the URL. Repo routes are /:owner/:repo/…;
// top-level routes (/, /settings) have no repo, so the tabs are hidden there.
function repoFromPath(pathname: string): { owner: string; repo: string } | null {
  const segs = pathname.split("/").filter(Boolean)
  if (segs.length < 2) return null
  return { owner: segs[0], repo: segs[1] }
}

export default function Layout({ children, onSignOut }: Props) {
  const { pathname } = useLocation()
  const ctx = repoFromPath(pathname)
  // Shares the repos-list / repo cache key, so this never fires an extra
  // request — it just reads the open-issue count the active page already loads.
  const repoQ = useRepo(ctx?.owner ?? "", ctx?.repo ?? "")

  function signOut() {
    clearToken()
    onSignOut()
  }

  return (
    <div className="app">
      <header className="topbar">
        <div className="topbar__lead">
          <Link to="/" className="brand">
            moongit
          </Link>
          {ctx && (
            <RepoTabs owner={ctx.owner} repo={ctx.repo} openIssues={repoQ.data?.open_issues} />
          )}
        </div>
        <div className="topbar__actions">
          <Link to="/settings" className="topbar__signout" title="Settings">
            settings
          </Link>
          <button type="button" className="topbar__signout" onClick={signOut} title="Forget token">
            sign out
          </button>
        </div>
      </header>
      <main className="main">{children}</main>
    </div>
  )
}
