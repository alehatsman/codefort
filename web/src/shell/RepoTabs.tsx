import { useLocation } from "react-router-dom"
import { useTabNav } from "@/shell/keyboardNav"
import { Tab, Tabs } from "@/ui"

interface Props {
  owner: string
  repo: string
  openIssues?: number
}

/**
 * Repo navigation tabs, rendered inline in the global top bar (see Layout) so
 * page content starts immediately below the header instead of after a separate
 * tab strip. The active tab is derived from the current URL. "Code" covers the
 * repo root plus the /tree/, /blob/ and /commits browser routes. List/Board
 * views both live under Issues — the switch between them is rendered inside the
 * Issues pages.
 *
 * Repo identity (owner/repo) is not shown here: it leads the unified breadcrumb
 * in the OverviewCard each page renders.
 *
 * Living on every repo route, this is also where h/l tab navigation is wired
 * (`useTabNav`), so the keys work consistently across all tabs.
 */
export default function RepoTabs({ owner, repo, openIssues }: Props) {
  const location = useLocation()
  useTabNav(owner, repo)
  const base = `/${owner}/${repo}`
  const isCode =
    location.pathname === base ||
    location.pathname.startsWith(`${base}/tree/`) ||
    location.pathname.startsWith(`${base}/blob/`) ||
    location.pathname.startsWith(`${base}/commits`)
  const isIssues = location.pathname.startsWith(`${base}/issues`)
  const isPulls =
    location.pathname.startsWith(`${base}/pulls`) || location.pathname.startsWith(`${base}/compare`)
  const isReview = location.pathname.startsWith(`${base}/review`)
  // Explore absorbed the former Research + Summaries tabs (and their URLs).
  const isExplore =
    location.pathname.startsWith(`${base}/explore`) ||
    location.pathname.startsWith(`${base}/research`) ||
    location.pathname.startsWith(`${base}/summaries`)
  const isSpecs = location.pathname.startsWith(`${base}/specs`)
  const isPipelines = location.pathname.startsWith(`${base}/pipelines`)
  const isAgents = location.pathname.startsWith(`${base}/agents`)
  const isSettings = location.pathname === `${base}/settings`

  return (
    <Tabs label="Repository navigation">
      <Tab to={base} active={isCode}>
        Code
      </Tab>
      <Tab to={`${base}/issues`} active={isIssues} count={openIssues}>
        Issues
      </Tab>
      <Tab to={`${base}/pulls`} active={isPulls}>
        Pull requests
      </Tab>
      <Tab to={`${base}/review`} active={isReview}>
        Review
      </Tab>
      <Tab to={`${base}/explore`} active={isExplore}>
        Explore
      </Tab>
      <Tab to={`${base}/specs`} active={isSpecs}>
        Specs
      </Tab>
      <Tab to={`${base}/pipelines`} active={isPipelines}>
        Pipelines
      </Tab>
      <Tab to={`${base}/agents`} active={isAgents}>
        Agents
      </Tab>
      <Tab to={`${base}/settings`} active={isSettings}>
        Settings
      </Tab>
    </Tabs>
  )
}
