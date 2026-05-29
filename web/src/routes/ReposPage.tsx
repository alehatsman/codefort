import { Link, useNavigate } from "react-router-dom"
import { useRepos } from "../api/queries"
import NewRepoForm from "../components/NewRepoForm"
import StateIcon from "../components/StateIcon"
import { useListNav } from "../lib/keyboardNav"

export default function ReposPage() {
  const { data, isLoading, error } = useRepos()
  const navigate = useNavigate()

  // hjkl roves the repo grid; Enter opens the selected repo.
  const { index } = useListNav({
    count: data?.length ?? 0,
    horizontal: true,
    onActivate: (i) => {
      const r = data?.[i]
      if (r) navigate(`/${r.owner}/${r.name}`)
    },
  })

  return (
    <div className="repos">
      <div className="issues__header">
        <div className="issues__header-left">
          <h2>Repositories</h2>
        </div>
        <NewRepoForm onCreated={(owner, name) => navigate(`/${owner}/${name}`)} />
      </div>

      {isLoading && <div className="loading">Loading…</div>}
      {error && <div className="error">{(error as Error).message}</div>}

      {!isLoading && !error && (!data || data.length === 0) && (
        <div className="empty">No repos registered yet. Create one with the button above.</div>
      )}

      {data && data.length > 0 && (
        <div className="card-grid">
          {data.map((r, i) => (
            <div
              key={r.id}
              className={`card ${i === index ? "is-vim-selected" : ""}`}
              data-vim-selected={i === index ? "true" : undefined}
            >
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
                  <span className="muted">
                    created {new Date(r.created_at).toLocaleDateString()}
                  </span>
                </span>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
