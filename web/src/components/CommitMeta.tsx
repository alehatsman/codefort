import { Link } from "react-router-dom"
import type { Commit } from "../api/types"
import Avatar from "./Avatar"
import { absoluteTime, timeAgo } from "../lib/timeAgo"

interface Props {
  owner: string
  repo: string
  commit: Commit
  className?: string
}

/**
 * Compact one-line commit summary — avatar, author, subject, short SHA and
 * relative time — used in the blob header. The subject links to the file's
 * commit history.
 */
export default function CommitMeta({ owner, repo, commit, className }: Props) {
  return (
    <div className={`commit-meta ${className ?? ""}`}>
      <Avatar name={commit.author} />
      <span className="commit-meta__author">{commit.author}</span>
      <Link to={`/${owner}/${repo}/commits`} className="commit-meta__subject" title={commit.subject}>
        {commit.subject}
      </Link>
      <code className="commit-meta__sha" title={commit.sha}>
        {commit.short_sha}
      </code>
      <span className="muted small" title={absoluteTime(commit.date)}>
        {timeAgo(commit.date)}
      </span>
    </div>
  )
}
