import clsx from "clsx"
import { Link } from "react-router-dom"
import type { CIRun, Commit } from "@/api/types"
import CommitCIStatus from "@/features/commits/CommitCIStatus"
import { Avatar, RelativeTime } from "@/ui"

interface Props {
  owner: string
  repo: string
  commit: Commit
  ciRun?: CIRun | undefined
  className?: string | undefined
}

/**
 * Compact one-line commit summary — avatar, author, subject, short SHA and
 * relative time — used in the blob header. The subject links to the file's
 * commit history.
 */
export default function CommitMeta({ owner, repo, commit, ciRun, className }: Props) {
  return (
    <div className={clsx("commit-meta", className)}>
      <Avatar name={commit.author} />
      <span className="commit-meta__author">{commit.author}</span>
      <Link
        to={`/${owner}/${repo}/commits`}
        className="commit-meta__subject"
        title={commit.subject}
      >
        {commit.subject}
      </Link>
      <code className="commit-meta__sha" title={commit.sha}>
        {commit.short_sha}
      </code>
      <RelativeTime className="muted small" iso={commit.date} />
      <CommitCIStatus owner={owner} repo={repo} run={ciRun} />
    </div>
  )
}
