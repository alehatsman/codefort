import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../api/client";

export default function ReposPage() {
  const { data, isLoading, error } = useQuery({
    queryKey: ["repos"],
    queryFn: () => api.listRepos(),
  });

  if (isLoading) return <div className="loading">Loading…</div>;
  if (error) return <div className="error">{(error as Error).message}</div>;
  if (!data || data.length === 0) {
    return (
      <div className="empty">
        No repos registered yet. Create one with{" "}
        <code>moongitd repo create &lt;owner&gt;/&lt;name&gt;</code>.
      </div>
    );
  }

  return (
    <div className="repos">
      <h2>Repositories</h2>
      <div className="card-grid">
        {data.map((r) => (
          <Link key={r.id} to={`/${r.owner}/${r.name}`} className="card">
            <div className="card__title">
              <span className="muted">{r.owner} /</span> {r.name}
            </div>
            <div className="card__meta">
              <span>{r.open_issues} open</span>
              <span className="muted">{r.total_issues} total</span>
            </div>
            <div className="card__date">
              created {new Date(r.created_at).toLocaleDateString()}
            </div>
          </Link>
        ))}
      </div>
    </div>
  );
}
