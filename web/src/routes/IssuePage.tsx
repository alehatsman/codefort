import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { api } from "../api/client";

export default function IssuePage() {
  const { owner = "", repo = "", number: numStr = "" } = useParams();
  const num = Number(numStr);

  const issueQ = useQuery({
    queryKey: ["issue", owner, repo, num],
    queryFn: () => api.getIssue(owner, repo, num),
    enabled: !!owner && !!repo && Number.isFinite(num),
  });

  const commentsQ = useQuery({
    queryKey: ["comments", owner, repo, num],
    queryFn: () => api.listComments(owner, repo, num),
    enabled: !!owner && !!repo && Number.isFinite(num),
  });

  if (issueQ.isLoading) return <div className="loading">Loading…</div>;
  if (issueQ.error) return <div className="error">{(issueQ.error as Error).message}</div>;
  if (!issueQ.data) return null;

  const iss = issueQ.data;
  return (
    <div className="issue">
      <nav className="crumbs">
        <Link to="/">repos</Link>
        <span className="muted"> / </span>
        <Link to={`/${owner}/${repo}`}>{owner} / {repo}</Link>
        <span className="muted"> / </span>
        <Link to={`/${owner}/${repo}/issues`}>issues</Link>
        <span className="muted"> / </span>
        <strong>#{iss.number}</strong>
      </nav>

      <h2>
        <span className="muted">#{iss.number}</span> {iss.title}
      </h2>

      <div className="meta">
        <span className={`badge badge--${iss.state}`}>{iss.state}</span>
        <span className="muted">opened by</span> <strong>{iss.author}</strong>
        <span className="muted">·</span>
        <span className="muted">{new Date(iss.created_at).toLocaleString()}</span>
        {iss.assignee && (
          <>
            <span className="muted">·</span>
            <span className="muted">assigned to</span>
            <strong>@{iss.assignee}</strong>
          </>
        )}
      </div>

      {iss.body && (
        <div className="body">
          <pre>{iss.body}</pre>
        </div>
      )}

      <h3>Comments</h3>
      {commentsQ.isLoading && <div className="loading">Loading comments…</div>}
      {commentsQ.data && commentsQ.data.length === 0 && (
        <div className="empty">No comments yet.</div>
      )}
      {commentsQ.data && commentsQ.data.length > 0 && (
        <ul className="comments">
          {commentsQ.data.map((c) => (
            <li key={c.id} className="comment">
              <div className="comment__head">
                <strong>{c.author}</strong>
                <span className="muted">{new Date(c.created_at).toLocaleString()}</span>
              </div>
              <div className="comment__body">
                <pre>{c.body}</pre>
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
