import { useParams } from "react-router-dom"
import { useComments, useIssue, useRepo, useWhoami } from "../api/queries"
import RepoHeader from "../components/RepoHeader"
import StateButtons from "../components/StateButtons"
import AssigneeControl from "../components/AssigneeControl"
import CommentForm from "../components/CommentForm"
import StateIcon from "../components/StateIcon"
import Avatar from "../components/Avatar"
import CommentItem from "../components/CommentItem"
import DeleteIssueButton from "../components/DeleteIssueButton"

export default function IssuePage() {
  const { owner = "", repo = "", number: numStr = "" } = useParams()
  const num = Number(numStr)

  const me = useWhoami()
  const repoQ = useRepo(owner, repo)
  const issueQ = useIssue(owner, repo, num)
  const commentsQ = useComments(owner, repo, num)

  if (issueQ.isLoading) return <div className="loading">Loading…</div>
  if (issueQ.error) return <div className="error">{(issueQ.error as Error).message}</div>
  if (!issueQ.data) return null

  const iss = issueQ.data

  return (
    <div>
      <RepoHeader owner={owner} repo={repo} openIssues={repoQ.data?.open_issues} />

      <h2 className="issue-title">
        {iss.title} <span className="issue-title__num">#{iss.number}</span>
      </h2>
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
          {iss.body && (
            <div className="body">
              <div className="body__head">
                <Avatar name={iss.author} /> {iss.author} •{" "}
                {new Date(iss.created_at).toLocaleString()}
              </div>
              <div className="body__content">{iss.body}</div>
            </div>
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
