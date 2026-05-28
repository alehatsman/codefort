import { Link } from "react-router-dom"
import { useRepos } from "../api/queries"
import StateIcon from "../components/StateIcon"

export default function ReposPage() {
  const { data, isLoading, error } = useRepos()

  if (isLoading) return <div className="loading">Loading…</div>
  if (error) return <div className="error">{(error as Error).message}</div>
  if (!data || data.length === 0) {
    return (
      <div className="empty">
        No repos registered yet. Create one with{" "}
        <code>moongitd repo create &lt;owner&gt;/&lt;name&gt;</code>.
      </div>
    )
  }

  return (
    <div className="repos">
      <h2>Repositories</h2>
      <div className="card-grid">
        {data.map((r) => (
          <div key={r.id} className="card">
            <div className="card__title">
              <Link to={`/${r.owner}/${r.name}`}>
                <span className="muted">{r.owner}/</span>
                {r.name}
              </Link>
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
                <span className="muted">created {new Date(r.created_at).toLocaleDateString()}</span>
              </span>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
