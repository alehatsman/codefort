import { Link, useLocation, useNavigate } from "react-router-dom"
import "./shell.css"
import { clearToken } from "@/api/client"
import GlobalTabs from "@/shell/GlobalTabs"
import RepoTabs from "@/shell/RepoTabs"
import { Menu } from "@/ui"

interface Props {
  children: React.ReactNode
  onSignOut: () => void
}

// First path segments owned by top-level (non-repo) routes. A repo can never be
// named one of these, so a path starting with one is never a repo context —
// this keeps multi-segment aggregate routes like /issues/board from being read
// as owner="issues"/repo="board" (which would show RepoTabs by mistake).
const TOP_LEVEL_SEGMENTS = new Set([
  "repos",
  "issues",
  "pulls",
  "pipelines",
  "agents",
  "settings",
  "dev",
])

// Derive the repo context from the URL. Repo routes are /:owner/:repo/…;
// top-level routes (/, /settings, /issues/board, …) have no repo, so the tabs
// are hidden / global there.
function repoFromPath(pathname: string): { owner: string; repo: string } | null {
  const segs = pathname.split("/").filter(Boolean)
  if (segs.length < 2) return null
  if (TOP_LEVEL_SEGMENTS.has(segs[0])) return null
  return { owner: segs[0], repo: segs[1] }
}

export default function Layout({ children, onSignOut }: Props) {
  const { pathname } = useLocation()
  const navigate = useNavigate()
  const ctx = repoFromPath(pathname)

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
          {ctx ? <RepoTabs owner={ctx.owner} repo={ctx.repo} /> : <GlobalTabs />}
        </div>
        <div className="topbar__actions">
          <Menu
            label="Account menu"
            align="end"
            trigger={
              <svg
                width="16"
                height="16"
                viewBox="0 0 16 16"
                fill="currentColor"
                aria-hidden="true"
              >
                <path d="M8 9a3 3 0 1 0 0-6 3 3 0 0 0 0 6Zm0 1.5c-2.5 0-5 1.25-5 3.25V15h10v-1.25c0-2-2.5-3.25-5-3.25Z" />
              </svg>
            }
            items={[
              {
                label: "settings",
                onSelect: () => navigate("/settings"),
                icon: (
                  <svg
                    width="14"
                    height="14"
                    viewBox="0 0 16 16"
                    fill="currentColor"
                    aria-hidden="true"
                  >
                    <path d="M8 10.5a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5Zm6.3-2.1.9.7-1 1.7-1.1-.4a4.7 4.7 0 0 1-.9.5l-.2 1.2H10l-.2-1.2a4.7 4.7 0 0 1-.9-.5l-1.1.4-1-1.7.9-.7a4.7 4.7 0 0 1 0-1l-.9-.7 1-1.7 1.1.4a4.7 4.7 0 0 1 .9-.5L10 3.7h2l.2 1.2a4.7 4.7 0 0 1 .9.5l1.1-.4 1 1.7-.9.7a4.7 4.7 0 0 1 0 1Z" />
                  </svg>
                ),
              },
              {
                label: "sign out",
                onSelect: signOut,
                icon: (
                  <svg
                    width="14"
                    height="14"
                    viewBox="0 0 16 16"
                    fill="currentColor"
                    aria-hidden="true"
                  >
                    <path d="M6 2h4a1 1 0 0 1 1 1v2H9.5V3.5h-3v9h3V11H11v2a1 1 0 0 1-1 1H6a1 1 0 0 1-1-1V3a1 1 0 0 1 1-1Zm5.2 4.4 2.1 1.6-2.1 1.6V8.75H7.5v-1.5h3.7V6.4Z" />
                  </svg>
                ),
              },
            ]}
          />
        </div>
      </header>
      <main className="main">{children}</main>
    </div>
  )
}
