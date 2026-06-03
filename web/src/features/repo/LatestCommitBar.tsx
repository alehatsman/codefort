import clsx from "clsx"
import { Link } from "react-router-dom"
import type { CIRun, Commit } from "@/api/types"
import CommitCIStatus from "@/features/commits/CommitCIStatus"
import { Avatar, RelativeTime } from "@/ui"

interface Props {
  owner: string
  repo: string
  path: string
  latest: Commit | null | undefined
  ciRun?: CIRun
  total: number
  loading: boolean
}

/**
 * GitHub-style bar above the file listing: the latest commit touching the
 * current directory on the left, a link to the full commit history on the
 * right. While the commit annotation query is in flight it shows a dim
 * placeholder so the file tree doesn't jump when the data arrives.
 */
export default function LatestCommitBar({
  owner,
  repo,
  path,
  latest,
  ciRun,
  total,
  loading,
}: Props) {
  const commitsHref = `/${owner}/${repo}/commits${path ? `/${path}` : ""}`

  return (
    <div className="latest-commit-bar">
      {latest ? (
        <div className="latest-commit-bar__commit">
          <Avatar name={latest.author} />
          <span className="latest-commit-bar__author">{latest.author}</span>
          <Link to={commitsHref} className="latest-commit-bar__subject" title={latest.subject}>
            {latest.subject}
          </Link>
          <code className="latest-commit-bar__sha" title={latest.sha}>
            {latest.short_sha}
          </code>
          <RelativeTime className="muted small" iso={latest.date} />
          <CommitCIStatus owner={owner} repo={repo} run={ciRun} />
        </div>
      ) : (
        <div className="latest-commit-bar__commit">
          <span className={clsx("latest-commit-bar__ph", { "is-loading": loading })}>
            {loading ? "" : "No commit history"}
          </span>
        </div>
      )}

      <Link to={commitsHref} className="latest-commit-bar__count">
        <strong>{total}</strong> {total === 1 ? "commit" : "commits"}
      </Link>
    </div>
  )
}
