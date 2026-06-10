import { lazy, Suspense, useState } from "react"
import "./issues.css"
import { Link, useParams } from "react-router-dom"
import { useComments, useIssue, useIssueCommits, useWhoami } from "@/api/queries"
import OverviewCard from "@/shell/OverviewCard"
import StateButtons from "@/features/issues/StateButtons"
import AssigneeControl from "@/features/issues/AssigneeControl"
import CommentForm from "@/features/issues/CommentForm"
import EditIssueForm from "@/features/issues/EditIssueForm"
import StateIcon from "@/features/issues/StateIcon"
import CommentItem from "@/features/issues/CommentItem"
import DeleteIssueButton from "@/features/issues/DeleteIssueButton"
import BranchTag from "@/features/repo/BranchTag"
import SpawnAgentButton from "@/features/agents/SpawnAgentButton"
import NotFound from "@/shell/NotFound"
import {
  Avatar,
  Badge,
  Button,
  DetailLayout,
  ErrorMessage,
  RelativeTime,
  SidebarSection,
  SkeletonText,
  Spinner,
} from "@/ui"
import type { ChildIssueSummary } from "@/api/types"

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

  if (issueQ.isLoading) return <SkeletonText lines={5} />
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
        <Badge state={iss.state}>
          <StateIcon state={iss.state} size={14} />
          {iss.state.replace("_", " ")}
        </Badge>
        <Avatar name={iss.author} />
        <strong>{iss.author}</strong>
        <span>opened this on {new Date(iss.created_at).toLocaleDateString()}</span>
      </div>

      {iss.parent_number != null && (
        <div className="issue-parent-link">
          Part of{" "}
          <Link to={`/${owner}/${repo}/issues/${iss.parent_number}`}>#{iss.parent_number}</Link>
        </div>
      )}

      <DetailLayout
        sidebar={
          <>
            <SidebarSection label="State">
              <StateButtons owner={owner} repo={repo} number={iss.number} current={iss.state} />
            </SidebarSection>
            {iss.labels.length > 0 && (
              <SidebarSection label="Labels">
                <div className="issue-labels">
                  {iss.labels.map((l) => (
                    <span key={l} className="issue-label">
                      {l}
                    </span>
                  ))}
                </div>
              </SidebarSection>
            )}
            <SidebarSection label="Assignee">
              <AssigneeControl
                owner={owner}
                repo={repo}
                number={iss.number}
                assignee={iss.assignee}
                state={iss.state}
                me={me.data?.name}
              />
            </SidebarSection>
            <SidebarSection label="Agent">
              <SpawnAgentButton owner={owner} repo={repo} number={iss.number} />
            </SidebarSection>
            <SidebarSection label="Danger zone">
              <DeleteIssueButton owner={owner} repo={repo} number={iss.number} />
            </SidebarSection>
          </>
        }
      >
        {editing ? (
          <EditIssueForm
            owner={owner}
            repo={repo}
            number={iss.number}
            initialTitle={iss.title}
            initialBody={iss.body ?? ""}
            initialLabels={iss.labels}
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

        {iss.children && iss.children.length > 0 && (
          <ChildIssueList owner={owner} repo={repo} items={iss.children} />
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
      </DetailLayout>
    </div>
  )
}

function ChildIssueList({
  owner,
  repo,
  items,
}: {
  owner: string
  repo: string
  items: ChildIssueSummary[]
}) {
  const done = items.filter((c) => c.state === "done" || c.state === "closed").length
  return (
    <section className="issue-children">
      <h3 className="issue-children__label">
        Sub-issues
        <span className="issue-children__progress">
          {done}/{items.length}
        </span>
      </h3>
      <ul className="issue-children__list">
        {items.map((c) => (
          <li key={c.number} className="issue-children__row">
            <StateIcon state={c.state} size={14} />
            <Link to={`/${owner}/${repo}/issues/${c.number}`} className="issue-children__title">
              {c.title}
            </Link>
            <span className="issue-children__num muted small">#{c.number}</span>
          </li>
        ))}
      </ul>
    </section>
  )
}
