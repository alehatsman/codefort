import { lazy, Suspense, useState } from "react"
import { Link, useParams } from "react-router-dom"
import { useComments, useIssue, useIssueCommits, useWhoami } from "@/api/queries"
import OverviewCard from "@/shell/OverviewCard"
import StateButtons from "@/features/issues/StateButtons"
import AssigneeControl from "@/features/issues/AssigneeControl"
import CommentForm from "@/features/issues/CommentForm"
import EditIssueForm from "@/features/issues/EditIssueForm"
import StateIcon from "@/features/issues/StateIcon"
import Avatar from "@/shell/Avatar"
import CommentItem from "@/features/issues/CommentItem"
import DeleteIssueButton from "@/features/issues/DeleteIssueButton"
import BranchTag from "@/features/repo/BranchTag"
import SpawnAgentButton from "@/features/agents/SpawnAgentButton"
import NotFound from "@/shell/NotFound"
import { Button, ErrorMessage, RelativeTime, Spinner } from "@/ui"

// The markdown renderer pulls in remark/rehype + the highlighter; load it only
// when an issue with a body is actually shown.
const Markdown = lazy(() => import("@/shell/Markdown"))

export default function IssuePage() {
  const { owner = "", repo = "", number: numStr = "" } = useParams()
  const num = Number(numStr)

  const me = useWhoami()
  const issueQ = useIssue(owner, repo, num)
  const commentsQ = useComments(owner, repo, num)
  const commitsQ = useIssueCommits(owner, repo, num)

  const [editing, setEditing] = useState(false)

  // A non-numeric path segment (e.g. /issues/new, which isn't a route) lands
  // here with num=NaN and the issue query idle — show NotFound, not a blank.
  if (!Number.isInteger(num) || num <= 0) {
    return (
      <NotFound
        title="Issue not found"
        detail={`There is no issue “${numStr}” in ${owner}/${repo}.`}
      />
    )
  }

  if (issueQ.isLoading) return <Spinner />
  if (issueQ.error) return <ErrorMessage error={issueQ.error} />
  if (!issueQ.data) return null

  const iss = issueQ.data

  return (
    <div>
      <OverviewCard owner={owner} repo={repo} path="" summaries={{}} />

      {!editing && (
        <h2 className="issue-title">
          {iss.title} <span className="issue-title__num">#{iss.number}</span>
          <Button
            variant="ghost"
            size="small"
            className="issue-title__edit"
            onClick={() => setEditing(true)}
          >
            Edit
          </Button>
        </h2>
      )}
      <div className="issue-subtitle">
        <span className={`badge badge--${iss.state}`}>
          <StateIcon state={iss.state} size={14} />
          {iss.state.replace("_", " ")}
        </span>
        <Avatar name={iss.author} />
        <strong>{iss.author}</strong>
        <span>opened this on {new Date(iss.created_at).toLocaleDateString()}</span>
      </div>

      <div className="issue-detail">
        <div className="issue-main">
          {editing ? (
            <EditIssueForm
              owner={owner}
              repo={repo}
              number={iss.number}
              initialTitle={iss.title}
              initialBody={iss.body ?? ""}
              onDone={() => setEditing(false)}
            />
          ) : (
            iss.body && (
              <div className="body">
                <div className="body__head">
                  <Avatar name={iss.author} /> {iss.author} •{" "}
                  {new Date(iss.created_at).toLocaleString()}
                </div>
                <div className="body__content">
                  <Suspense fallback={<div className="markdown-body loading">Loading…</div>}>
                    <Markdown content={iss.body} owner={owner} repo={repo} basePath="" />
                  </Suspense>
                </div>
              </div>
            )
          )}

          {commentsQ.isLoading && <Spinner label="Loading comments…" />}
          {commentsQ.data && commentsQ.data.length > 0 && (
            <ul className="comments">
              {commentsQ.data.map((c) => (
                <CommentItem
                  key={c.id}
                  owner={owner}
                  repo={repo}
                  issueNumber={iss.number}
                  comment={c}
                  canDelete={me.data?.name === c.author}
                />
              ))}
            </ul>
          )}

          {commitsQ.data && commitsQ.data.length > 0 && (
            <section className="issue-commits">
              <h3 className="issue-commits__label">
                Commits <span className="issue-commits__count">{commitsQ.data.length}</span>
              </h3>
              <ul className="commit-list">
                {commitsQ.data.map((c) => {
                  const to = `/${owner}/${repo}/commit/${c.sha}`
                  return (
                    <li key={c.sha} className="commit-row">
                      <Avatar name={c.author} />
                      <div className="commit-row__main">
                        <Link to={to} className="commit-row__subject" title={c.subject}>
                          {c.subject}
                        </Link>
                        <div className="commit-row__meta muted small">
                          <span className="commit-row__author">{c.author}</span>
                          {" committed "}
                          <RelativeTime iso={c.date} />
                        </div>
                      </div>
                      {c.branch && <BranchTag branch={c.branch} />}
                      <Link to={to} className="commit-row__sha" title={`View commit ${c.sha}`}>
                        {c.short_sha}
                      </Link>
                    </li>
                  )
                })}
              </ul>
            </section>
          )}

          <CommentForm owner={owner} repo={repo} number={iss.number} />
        </div>

        <aside className="sidebar">
          <section className="sidebar__section">
            <h3 className="sidebar__label">State</h3>
            <StateButtons owner={owner} repo={repo} number={iss.number} current={iss.state} />
          </section>
          <section className="sidebar__section">
            <h3 className="sidebar__label">Assignee</h3>
            <AssigneeControl
              owner={owner}
              repo={repo}
              number={iss.number}
              assignee={iss.assignee}
              state={iss.state}
              me={me.data?.name}
            />
          </section>
          <section className="sidebar__section">
            <h3 className="sidebar__label">Agent</h3>
            <SpawnAgentButton owner={owner} repo={repo} number={iss.number} />
          </section>
          <section className="sidebar__section">
            <h3 className="sidebar__label">Danger zone</h3>
            <DeleteIssueButton owner={owner} repo={repo} number={iss.number} />
          </section>
        </aside>
      </div>
    </div>
  )
}
