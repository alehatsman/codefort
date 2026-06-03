import { useRef } from "react"
import "./repo.css"
import { Link, useNavigate } from "react-router-dom"
import { useRepos } from "@/api/queries"
import CIStatusIcon from "@/features/pipelines/CIStatusIcon"
import NewRepoForm from "@/features/repo/NewRepoForm"
import { Card, EmptyState, ErrorMessage, PageHeader, Spinner } from "@/ui"
import type { Repo } from "@/api/types"
import { useListNav } from "@/shell/keyboardNav"

export default function ReposPage() {
  const { data, isLoading, error } = useRepos()
  const navigate = useNavigate()

  // hjkl roves the repo grid (j/k a row, h/l a cell); Enter opens the repo.
  // Columns are read live from the rendered grid so nav follows reflow.
  const gridRef = useRef<HTMLDivElement>(null)
  const { index } = useListNav({
    count: data?.length ?? 0,
    getColumns: () => gridColumnCount(gridRef.current),
    onActivate: (i) => {
      const r = data?.[i]
      if (r) navigate(`/${r.owner}/${r.name}`)
    },
  })

  return (
    <div className="repos">
      <PageHeader
        title="Repositories"
        actions={<NewRepoForm onCreated={(owner, name) => navigate(`/${owner}/${name}`)} />}
      />

      {isLoading && <Spinner />}
      {error && <ErrorMessage error={error} />}

      {!isLoading && !error && (!data || data.length === 0) && (
        <EmptyState>No repos registered yet. Create one with the button above.</EmptyState>
      )}

      {data && data.length > 0 && (
        <div className="card-grid" ref={gridRef}>
          {data.map((r, i) => (
            <Card
              key={r.id}
              selected={i === index}
              data-vim-selected={i === index ? "true" : undefined}
            >
              <div className="card__title">
                <Link to={`/${r.owner}/${r.name}`}>
                  <span className="muted">{r.owner}/</span>
                  {r.name}
                </Link>
                <span className="card__created muted">
                  {new Date(r.created_at).toLocaleDateString()}
                </span>
              </div>
              <RepoMetrics repo={r} />
            </Card>
          ))}
        </div>
      )}
    </div>
  )
}

// RepoMetrics renders the five at-a-glance metrics in fixed order —
// CI · Issues · PRs · Reviews · Agents — as a labeled tile grid. Each tile
// deep-links to the repo's matching sub-page; a zero count is dimmed so the
// repos with live activity stand out at a glance.
function RepoMetrics({ repo: r }: { repo: Repo }) {
  const base = `/${r.owner}/${r.name}`
  const ciTo = r.ci_status ? `${base}/pipelines/${r.ci_number}` : `${base}/pipelines`
  return (
    <div className="repo-metrics">
      <Link className="repo-metric" to={ciTo} title={r.ci_status ? `CI ${r.ci_status}` : "CI"}>
        <span className="repo-metric__value">
          {r.ci_status ? <CIStatusIcon status={r.ci_status} /> : <span className="muted">—</span>}
        </span>
        <span className="repo-metric__label">CI</span>
      </Link>
      <MetricTile to={`${base}/issues`} value={r.open_issues} label="Issues" />
      <MetricTile to={`${base}/pulls`} value={r.open_pulls} label="PRs" />
      <MetricTile to={`${base}/review`} value={r.open_reviews} label="Reviews" />
      <MetricTile to={`${base}/agents`} value={r.active_agents} label="Agents" />
    </div>
  )
}

function MetricTile({ to, value, label }: { to: string; value: number; label: string }) {
  return (
    <Link className="repo-metric" to={to} title={`${value} ${label.toLowerCase()}`}>
      <span className="repo-metric__value" data-zero={value === 0 ? "true" : undefined}>
        {value}
      </span>
      <span className="repo-metric__label">{label}</span>
    </Link>
  )
}

// Count the columns the CSS grid currently renders. getComputedStyle resolves
// the auto-fill template to explicit pixel tracks ("312px 312px 312px"), so
// the track count is the live column count at this viewport width.
function gridColumnCount(grid: HTMLDivElement | null): number {
  if (!grid) return 1
  const tracks = getComputedStyle(grid).gridTemplateColumns.split(" ").filter(Boolean)
  return tracks.length || 1
}
