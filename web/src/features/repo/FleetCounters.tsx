import { useMemo } from "react"
import { Link } from "react-router-dom"
import { useAllIssues, useRepos } from "@/api/queries"
import type { Repo } from "@/api/types"

// FleetCounters renders a row of live stat tiles at the top of the homepage.
// Most counters are derived from the repos list (already fetched) to avoid
// extra round-trips. In-progress issues need a small separate query since the
// repo summary only exposes open_issues (todo+in_progress combined).
export default function FleetCounters() {
  const { data: repos } = useRepos()
  const { data: inProgress } = useAllIssues("state=in_progress")

  const stats = useMemo(() => compute(repos ?? []), [repos])

  return (
    <div className="fleet-counters">
      <Tile to="/agents" value={stats.agentsRunning} label="Agents running" />
      <Tile to="/issues" value={inProgress?.length ?? 0} label="In progress" />
      <Tile to="/pipelines" value={stats.ciFailing} label="CI failing" suffix=" repos" />
      <Tile to="/pulls" value={stats.openPRs} label="Open PRs" />
    </div>
  )
}

function compute(repos: Repo[]) {
  let agentsRunning = 0
  let ciFailing = 0
  let openPRs = 0
  for (const r of repos) {
    agentsRunning += r.active_agents
    openPRs += r.open_pulls
    if (r.ci_status === "failed" || r.ci_status === "error") ciFailing++
  }
  return { agentsRunning, ciFailing, openPRs }
}

function Tile({
  to,
  value,
  label,
  suffix = "",
}: {
  to: string
  value: number
  label: string
  suffix?: string
}) {
  return (
    <Link className="fleet-counter" to={to} title={`${value}${suffix} ${label.toLowerCase()}`}>
      <span className="fleet-counter__value" data-zero={value === 0 ? "true" : undefined}>
        {value}
        {suffix && <span className="fleet-counter__suffix">{suffix}</span>}
      </span>
      <span className="fleet-counter__label">{label}</span>
    </Link>
  )
}
