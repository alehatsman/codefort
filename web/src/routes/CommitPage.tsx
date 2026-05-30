import { useState } from "react"
import { Link, useParams } from "react-router-dom"
import { useCommit, useRepo } from "../api/queries"
import RepoHeader from "../components/RepoHeader"
import OverviewCard from "../components/OverviewCard"
import Avatar from "../components/Avatar"
import DiffView from "../components/DiffView"
import { absoluteTime, timeAgo } from "../lib/timeAgo"

type Mode = "split" | "unified"

/**
 * Commit detail: message header, a "N files changed" summary bar with a
 * Split/Unified toggle, and a DiffView per changed file. The diff is fetched
 * from the commit endpoint (metadata + structured hunks); split is the default
 * layout per the GitHub-style side-by-side request.
 */
export default function CommitPage() {
  const { owner = "", repo = "", sha = "" } = useParams()
  const repoQ = useRepo(owner, repo)
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
      <RepoHeader owner={owner} repo={repo} openIssues={repoQ.data?.open_issues} />
      <OverviewCard owner={owner} repo={repo} path="" summary="" />

      {commitQ.isLoading ? (
        <div className="loading">Loading…</div>
      ) : commitQ.error ? (
        <div className="error">{(commitQ.error as Error).message}</div>
      ) : !detail ? (
        <div className="empty">Commit not found.</div>
      ) : (
        <>
          <header className="commit-detail__head">
            <h2 className="commit-detail__subject">{detail.commit.subject}</h2>
            {detail.commit.body && <pre className="commit-detail__body">{detail.commit.body}</pre>}
            <div className="commit-detail__meta muted small">
              <Avatar name={detail.commit.author} />
              <span className="commit-detail__author">{detail.commit.author}</span>
              {" committed "}
              <span title={absoluteTime(detail.commit.date)}>{timeAgo(detail.commit.date)}</span>
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
            <div className="diff-summary__toggle" role="group" aria-label="Diff layout">
              <button
                type="button"
                className={mode === "split" ? "is-active" : ""}
                aria-pressed={mode === "split"}
                onClick={() => setMode("split")}
              >
                Split
              </button>
              <button
                type="button"
                className={mode === "unified" ? "is-active" : ""}
                aria-pressed={mode === "unified"}
                onClick={() => setMode("unified")}
              >
                Unified
              </button>
            </div>
          </div>

          {detail.truncated && (
            <div className="diff-truncated">
              This diff is large and was truncated; some changes are not shown.
            </div>
          )}

          {detail.files.length === 0 ? (
            <div className="empty">No file changes in this commit.</div>
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
