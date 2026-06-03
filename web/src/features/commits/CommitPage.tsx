import { useState } from "react"
import "./commits.css"
import { Link, useParams } from "react-router-dom"
import { useCommit } from "@/api/queries"
import OverviewCard from "@/shell/OverviewCard"
import BranchTag from "@/features/repo/BranchTag"
import DiffView from "@/features/pulls/DiffView"
import {
  Avatar,
  EmptyState,
  ErrorMessage,
  RelativeTime,
  SegmentedControl,
  SkeletonText,
} from "@/ui"

type Mode = "split" | "unified"

/**
 * Commit detail: message header, a "N files changed" summary bar with a
 * Split/Unified toggle, and a DiffView per changed file. The diff is fetched
 * from the commit endpoint (metadata + structured hunks); split is the default
 * layout per the GitHub-style side-by-side request.
 */
export default function CommitPage() {
  const { owner = "", repo = "", sha = "" } = useParams()
  const commitQ = useCommit(owner, repo, sha)
  const [mode, setMode] = useState<Mode>("split")
  const [copied, setCopied] = useState(false)

  const detail = commitQ.data

  function copySha() {
    navigator.clipboard?.writeText(detail?.commit.sha ?? sha).then(
      () => {
        setCopied(true)
        setTimeout(() => setCopied(false), 1200)
      },
      () => {}
    )
  }

  return (
    <div className="commit-page">
      <OverviewCard owner={owner} repo={repo} path="" summaries={{}} />

      {commitQ.isLoading ? (
        <SkeletonText lines={4} />
      ) : commitQ.error ? (
        <ErrorMessage error={commitQ.error} />
      ) : !detail ? (
        <EmptyState>Commit not found.</EmptyState>
      ) : (
        <>
          <header className="commit-detail__head">
            <h2 className="commit-detail__subject">{detail.commit.subject}</h2>
            {detail.commit.body && <pre className="commit-detail__body">{detail.commit.body}</pre>}
            <div className="commit-detail__meta muted small">
              <Avatar name={detail.commit.author} />
              <span className="commit-detail__author">{detail.commit.author}</span>
              {" committed "}
              <RelativeTime iso={detail.commit.date} />
              <span className="commit-detail__sha-group">
                <button
                  type="button"
                  className="commit-detail__sha"
                  title={copied ? "Copied!" : "Copy full SHA"}
                  onClick={copySha}
                >
                  {copied ? "✓ copied" : detail.commit.short_sha}
                </button>
              </span>
              {detail.commit.branch && <BranchTag branch={detail.commit.branch} />}
              {detail.parents.length > 0 && (
                <span className="commit-detail__parents">
                  {detail.parents.length > 1 ? "parents " : "parent "}
                  {detail.parents.map((p) => (
                    <Link
                      key={p}
                      to={`/${owner}/${repo}/commit/${p}`}
                      className="commit-detail__parent"
                    >
                      {p.slice(0, 7)}
                    </Link>
                  ))}
                </span>
              )}
            </div>
          </header>

          <div className="diff-summary">
            <span className="diff-summary__counts">
              {detail.files.length} {detail.files.length === 1 ? "file" : "files"} changed
              {detail.additions > 0 && <span className="diff-file__add"> +{detail.additions}</span>}
              {detail.deletions > 0 && <span className="diff-file__del"> −{detail.deletions}</span>}
            </span>
            <SegmentedControl
              label="Diff layout"
              orientation="row"
              value={mode}
              onChange={setMode}
              options={[
                { value: "split", label: "Split" },
                { value: "unified", label: "Unified" },
              ]}
            />
          </div>

          {detail.truncated && (
            <div className="diff-truncated">
              This diff is large and was truncated; some changes are not shown.
            </div>
          )}

          {detail.files.length === 0 ? (
            <EmptyState>No file changes in this commit.</EmptyState>
          ) : (
            detail.files.map((f) => (
              <DiffView key={f.new_path || f.old_path} file={f} mode={mode} />
            ))
          )}
        </>
      )}
    </div>
  )
}
