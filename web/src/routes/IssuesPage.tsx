import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { api } from "../api/client";
import type { IssueState } from "../api/types";

const STATES: IssueState[] = ["todo", "in_progress", "done", "closed"];

export default function IssuesPage() {
  const { owner = "", repo = "" } = useParams();
  const [activeStates, setActiveStates] = useState<IssueState[]>(["todo", "in_progress"]);
  const [unassignedOnly, setUnassignedOnly] = useState(false);

  const query = new URLSearchParams();
  if (activeStates.length > 0) query.set("state", activeStates.join(","));
  if (unassignedOnly) query.set("assignee", "null");

  const { data, isLoading, error } = useQuery({
    queryKey: ["issues", owner, repo, query.toString()],
    queryFn: () => api.listIssues(owner, repo, query.toString()),
    enabled: !!owner && !!repo,
  });

  function toggleState(s: IssueState) {
    setActiveStates((prev) =>
      prev.includes(s) ? prev.filter((x) => x !== s) : [...prev, s]
    );
  }

  return (
    <div className="issues">
      <nav className="crumbs">
        <Link to="/">repos</Link>
        <span className="muted"> / </span>
        <Link to={`/${owner}/${repo}`}>{owner} / {repo}</Link>
        <span className="muted"> / </span>
        <strong>issues</strong>
      </nav>

      <h2>Issues</h2>

      <div className="filters">
        <div className="filter-row">
          <span className="filter-label">state:</span>
          {STATES.map((s) => (
            <label key={s} className="chip">
              <input
                type="checkbox"
                checked={activeStates.includes(s)}
                onChange={() => toggleState(s)}
              />
              {s}
            </label>
          ))}
        </div>
        <label className="chip">
          <input
            type="checkbox"
            checked={unassignedOnly}
            onChange={(e) => setUnassignedOnly(e.target.checked)}
          />
          unassigned only
        </label>
      </div>

      {isLoading && <div className="loading">Loading…</div>}
      {error && <div className="error">{(error as Error).message}</div>}

      {data && data.length === 0 && (
        <div className="empty">No issues match these filters.</div>
      )}

      {data && data.length > 0 && (
        <ul className="issue-list">
          {data.map((iss) => (
            <li key={iss.id} className="issue-row">
              <Link
                to={`/${owner}/${repo}/issues/${iss.number}`}
                className="issue-row__link"
              >
                <span className={`badge badge--${iss.state}`}>{iss.state}</span>
                <span className="issue-row__num">#{iss.number}</span>
                <span className="issue-row__title">{iss.title}</span>
                <span className="issue-row__assignee muted">
                  {iss.assignee ? `@${iss.assignee}` : "—"}
                </span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
