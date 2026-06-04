import { useLocation } from "react-router-dom"
import { Tab, Tabs } from "@/ui"

/**
 * Top-level (non-repo) navigation, rendered inline in the global top bar by
 * Layout whenever the URL has no repo context. It mirrors RepoTabs but the
 * tabs are fleet-wide aggregate views: Repos (the index), and the cross-repo
 * Issues / Pull requests / Pipelines / Agents feeds. The active tab is derived
 * from the current URL.
 */
export default function GlobalTabs() {
  const { pathname } = useLocation()
  const isRepos = pathname === "/repos"
  const isIssues = pathname.startsWith("/issues")
  const isPulls = pathname.startsWith("/pulls")
  const isPipelines = pathname.startsWith("/pipelines")
  const isAgents = pathname.startsWith("/agents")

  return (
    <Tabs label="Global navigation">
      <Tab to="/repos" active={isRepos}>
        Repos
      </Tab>
      <Tab to="/issues/board" active={isIssues}>
        Issues
      </Tab>
      <Tab to="/pulls" active={isPulls}>
        Pull requests
      </Tab>
      <Tab to="/pipelines" active={isPipelines}>
        Pipelines
      </Tab>
      <Tab to="/agents" active={isAgents}>
        Agents
      </Tab>
    </Tabs>
  )
}
