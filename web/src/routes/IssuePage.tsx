import { lazy, Suspense, useState } from "react"
import { Link, useParams } from "react-router-dom"
import { useComments, useIssue, useIssueCommits, useRepo, useWhoami } from "../api/queries"
import { absoluteTime, timeAgo } from "../lib/timeAgo"
import RepoHeader from "../components/RepoHeader"
import OverviewCard from "../components/OverviewCard"
import StateButtons from "../components/StateButtons"
import AssigneeControl from "../components/AssigneeControl"
import CommentForm from "../components/CommentForm"
import EditIssueForm from "../components/EditIssueForm"
import StateIcon from "../components/StateIcon"
import Avatar from "../components/Avatar"
import CommentItem from "../components/CommentItem"
import DeleteIssueButton from "../components/DeleteIssueButton"
import BranchTag from "../components/BranchTag"

// The markdown renderer pulls in remark/rehype + the highlighter; load it only
// when an issue with a body is actually shown.
const Markdown = lazy(() => import("../components/Markdown"))

export default function IssuePage() {
  const { owner = "", repo = "", number: numStr = "" } = useParams()
  const num = Number(numStr)

  const me = useWhoami()
  const repoQ = useRepo(owner, repo)
  const issueQ = useIssue(owner, repo, num)
  const commentsQ = useComments(owner, repo, num)
  const commitsQ = useIssueCommits(owner, repo, num)

  const [editing, setEditing] = useState(false)

  if (issueQ.isLoading) return <div className="loading">Loading…</div>
  if (issueQ.error) return <div className="error">{(issueQ.error as Error).message}</div>
  if (!issueQ.data) return null

  const iss = issueQ.data

  return (
    <div>
      <RepoHeader owner={owner} repo={repo} openIssues={repoQ.data?.open_issues} />
      <OverviewCard owner={owner} repo={repo} path="" summaries={{}} />

      {!editing && (
        <h2 className="issue-title">
          {iss.title} <span className="issue-title__num">#{iss.number}</span>
          <button
            type="button"
            className="btn btn--ghost btn--sm issue-title__edit"
            onClick={() => setEditing(true)}
          >
            Edit
          </button>
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

          {commentsQ.isLoading && <div className="loading">Loading comments…</div>}
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
                          <span title={absoluteTime(c.date)}>{timeAgo(c.date)}</span>
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
            <h3 className="sidebar__label">Danger zone</h3>
            <DeleteIssueButton owner={owner} repo={repo} number={iss.number} />
          </section>
        </aside>
      </div>
    </div>
  )
}
