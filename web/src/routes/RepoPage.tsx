import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { api } from "../api/client";

export default function RepoPage() {
  const { owner = "", repo = "" } = useParams();

  const repoQ = useQuery({
    queryKey: ["repo", owner, repo],
    queryFn: () => api.getRepo(owner, repo),
    enabled: !!owner && !!repo,
  });

  if (repoQ.isLoading) return <div className="loading">Loading…</div>;
  if (repoQ.error) return <div className="error">{(repoQ.error as Error).message}</div>;
  if (!repoQ.data) return null;

  const r = repoQ.data;
  return (
    <div className="repo">
      <nav className="crumbs">
        <Link to="/">repos</Link>
        <span className="muted"> / </span>
        <span>{r.owner}</span>
        <span className="muted"> / </span>
        <strong>{r.name}</strong>
      </nav>

      <h2>{r.owner} / {r.name}</h2>
      <div className="muted">created {new Date(r.created_at).toLocaleString()}</div>

      <div className="stats">
        <Link to={`/${r.owner}/${r.name}/issues`} className="stat">
          <div className="stat__value">{r.open_issues}</div>
          <div className="stat__label">open issues</div>
        </Link>
        <div className="stat">
          <div className="stat__value">{r.total_issues}</div>
          <div className="stat__label">total issues</div>
        </div>
      </div>

      <p className="muted small">
        Code browser is on the roadmap. For now: clone with{" "}
        <code>git clone http://localhost:8080/{r.owner}/{r.name}.git</code>
      </p>
    </div>
  );
}
