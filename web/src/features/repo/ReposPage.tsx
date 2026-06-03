import { useRef } from "react"
import "./repo.css"
import { Link, useNavigate } from "react-router-dom"
import { useRepos } from "@/api/queries"
import CIStatusIcon from "@/features/pipelines/CIStatusIcon"
import NewRepoForm from "@/features/repo/NewRepoForm"
import StateIcon from "@/features/issues/StateIcon"
import { Card, EmptyState, ErrorMessage, PageHeader, Spinner } from "@/ui"
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
                {r.ci_status && (
                  <Link
                    to={`/${r.owner}/${r.name}/pipelines/${r.ci_number}`}
                    className="card__ci"
                    title={`CI ${r.ci_status}`}
                  >
                    <CIStatusIcon status={r.ci_status} />
                  </Link>
                )}
              </div>
              <div className="card__meta">
                <span className="card__meta-item">
                  <StateIcon state="todo" />
                  {r.open_issues} open
                </span>
                <span className="card__meta-item">
                  <span className="muted">{r.total_issues} total</span>
                </span>
                <span className="card__meta-item">
                  <span className="muted">
                    created {new Date(r.created_at).toLocaleDateString()}
                  </span>
                </span>
              </div>
            </Card>
          ))}
        </div>
      )}
    </div>
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
