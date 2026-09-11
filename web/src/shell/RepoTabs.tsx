import { useLocation } from "react-router-dom"
import { useCIRuns, useCodeComments, useRepo, useSpecsList } from "@/api/queries"
import type { CIRun } from "@/api/types"
import { useTabNav } from "@/shell/keyboardNav"
import { Tab, Tabs } from "@/ui"

interface Props {
  owner: string
  repo: string
}

// A CI run still in flight (queued / running / finishing). Drives the Pipelines
// count — only live runs are worth flagging on the tab.
function isInFlight(status: CIRun["status"]): boolean {
  return status === "queued" || status === "running" || status === "finishing"
}

// An agent run that's active: in flight, or parked awaiting a human turn
// (awaiting_input is idle but still "open", so it counts on the Agents tab).
function isActiveAgent(status: CIRun["status"]): boolean {
  return isInFlight(status) || status === "awaiting_input"
}

// A count is shown only when there's something to flag — a "0" pill is noise.
function pill(n: number | undefined): number | undefined {
  return n && n > 0 ? n : undefined
}

/**
 * Repo navigation tabs, rendered inline in the global top bar (see Layout) so
 * page content starts immediately below the header instead of after a separate
 * tab strip. The active tab is derived from the current URL. "Code" covers the
 * repo root plus the /tree/, /blob/ and /commits browser routes. List/Board
 * views both live under Issues — the switch between them is rendered inside the
 * Issues pages.
 *
 * Each tab carries a count pill of the work waiting there (open issues / review
 * comments, total specs, in-flight pipelines, active agents). The queries share
 * their pages' cache keys, so a tab's count is already warm once you visit it;
 * the runs queries poll while anything is live, so the pill ticks on its own.
 *
 * Repo identity (owner/repo) is not shown here: it leads the unified breadcrumb
 * in the OverviewCard each page renders.
 *
 * Living on every repo route, this is also where h/l tab navigation is wired
 * (`useTabNav`), so the keys work consistently across all tabs.
 */
export default function RepoTabs({ owner, repo }: Props) {
  const location = useLocation()
  useTabNav(owner, repo)

  const repoQ = useRepo(owner, repo)
  const specsQ = useSpecsList(owner, repo)
  const reviewQ = useCodeComments(owner, repo, { state: "open" })
  const ciQ = useCIRuns(owner, repo, "ci")
  const agentQ = useCIRuns(owner, repo, "agent")

  const openIssues = repoQ.data?.open_issues
  const openReview = reviewQ.data?.length
  const specCount = specsQ.data?.specs.length
  const livePipelines = ciQ.data?.filter((r) => isInFlight(r.status)).length
  const activeAgents = agentQ.data?.filter((r) => isActiveAgent(r.status)).length

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
  // const isExplore =
  //   location.pathname.startsWith(`${base}/explore`) ||
  //   location.pathname.startsWith(`${base}/research`) ||
  //   location.pathname.startsWith(`${base}/summaries`)
  const isSpecs = location.pathname.startsWith(`${base}/specs`)
  const isBranches =
    location.pathname.startsWith(`${base}/branches`) || location.pathname.startsWith(`${base}/tags`)
  const isPipelines = location.pathname.startsWith(`${base}/pipelines`)
  const isAgents = location.pathname.startsWith(`${base}/agents`)

  return (
    <Tabs label="Repository navigation">
      <Tab to={base} active={isCode}>
        Code
      </Tab>
      <Tab to={`${base}/specs`} active={isSpecs} count={pill(specCount)}>
        Specs
      </Tab>
      <Tab to={`${base}/issues/board`} active={isIssues} count={pill(openIssues)}>
        Issues
      </Tab>
      <Tab to={`${base}/pulls`} active={isPulls}>
        Pull requests
      </Tab>
      <Tab to={`${base}/branches`} active={isBranches}>
        Branches
      </Tab>
      <Tab to={`${base}/review`} active={isReview} count={pill(openReview)}>
        Review
      </Tab>
      {/* <Tab to={`${base}/explore`} active={isExplore}> */}
      {/*   Explore */}
      {/* </Tab> */}
      <Tab to={`${base}/pipelines`} active={isPipelines} count={pill(livePipelines)}>
        Pipelines
      </Tab>
      <Tab to={`${base}/agents`} active={isAgents} count={pill(activeAgents)}>
        Agents
      </Tab>
      <Tab to={`${base}/settings`} active={location.pathname.startsWith(`${base}/settings`)}>
        Settings
      </Tab>
    </Tabs>
  )
}
