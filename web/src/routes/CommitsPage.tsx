import { Link, useParams } from "react-router-dom"
import { useCommitCIStatus, useInfiniteCommits, useRepo } from "../api/queries"
import type { CIRun, Commit } from "../api/types"
import RepoHeader from "../components/RepoHeader"
import OverviewCard from "../components/OverviewCard"
import Avatar from "../components/Avatar"
import CommitCIStatus from "../components/CommitCIStatus"
import { absoluteTime, timeAgo } from "../lib/timeAgo"

const PER_PAGE = 30

/**
 * Commit history for the repo's default branch, optionally scoped to a path
 * (the splat param). GitHub-style: rows grouped by calendar day, newest
 * first, with a "Load more" button that pages through the history.
 */
export default function CommitsPage() {
  const { owner = "", repo = "" } = useParams()
  const path = useParams()["*"] ?? ""

  const repoQ = useRepo(owner, repo)
  const commitsQ = useInfiniteCommits(owner, repo, { path, perPage: PER_PAGE })
  const ciStatusQ = useCommitCIStatus(owner, repo, repoQ.data?.ci_enabled ?? false)

  // Flatten the loaded pages into one list, deduped by SHA — page boundaries
  // can shift if new commits land between fetches.
  const commits = dedupeBySha(commitsQ.data?.pages.flatMap((p) => p.commits) ?? [])
  const groups = groupByDay(commits)

  return (
    <div className="commits-page">
      <RepoHeader owner={owner} repo={repo} openIssues={repoQ.data?.open_issues} />
      <OverviewCard owner={owner} repo={repo} path={path} summaries={{}} />

      <div className="commits-page__head">
        <h2>Commits</h2>
      </div>

      {commitsQ.isLoading && commits.length === 0 ? (
        <div className="loading">Loading…</div>
      ) : commitsQ.error ? (
        <div className="error">{(commitsQ.error as Error).message}</div>
      ) : commits.length === 0 ? (
        <div className="empty">No commit history.</div>
      ) : (
        <>
          {groups.map((g) => (
            <section key={g.day} className="commit-group">
              <h3 className="commit-group__day">Commits on {g.day}</h3>
              <ul className="commit-list">
                {g.commits.map((c) => (
                  <CommitRow
                    key={c.sha}
                    owner={owner}
                    repo={repo}
                    commit={c}
                    ciRun={ciStatusQ.data?.get(c.sha)}
                  />
                ))}
              </ul>
            </section>
          ))}

          {commitsQ.hasNextPage && (
            <button
              type="button"
              className="commits-page__more"
              disabled={commitsQ.isFetchingNextPage}
              onClick={() => commitsQ.fetchNextPage()}
            >
              {commitsQ.isFetchingNextPage ? "Loading…" : "Load more"}
            </button>
          )}
        </>
      )}
    </div>
  )
}

function CommitRow({
  owner,
  repo,
  commit,
  ciRun,
}: {
  owner: string
  repo: string
  commit: Commit
  ciRun?: CIRun
}) {
  const to = `/${owner}/${repo}/commit/${commit.sha}`
  return (
    <li className="commit-row">
      <Avatar name={commit.author} />
      <div className="commit-row__main">
        <Link to={to} className="commit-row__subject" title={commit.subject}>
          {commit.subject}
        </Link>
        <div className="commit-row__meta muted small">
          <span className="commit-row__author">{commit.author}</span>
          {" committed "}
          <span title={absoluteTime(commit.date)}>{timeAgo(commit.date)}</span>
        </div>
      </div>
      <CommitCIStatus owner={owner} repo={repo} run={ciRun} />
      <Link to={to} className="commit-row__sha" title={`View commit ${commit.sha}`}>
        {commit.short_sha}
      </Link>
    </li>
  )
}

function dedupeBySha(commits: Commit[]): Commit[] {
  const seen = new Set<string>()
  return commits.filter((c) => {
    if (seen.has(c.sha)) return false
    seen.add(c.sha)
    return true
  })
}

interface DayGroup {
  day: string
  commits: Commit[]
}

// groupByDay splits an already-sorted (newest-first) commit list into
// consecutive same-day runs, preserving order.
function groupByDay(commits: Commit[]): DayGroup[] {
  const groups: DayGroup[] = []
  for (const c of commits) {
    const day = new Date(c.date).toLocaleDateString(undefined, {
      year: "numeric",
      month: "long",
      day: "numeric",
    })
    const last = groups[groups.length - 1]
    if (last && last.day === day) {
      last.commits.push(c)
    } else {
      groups.push({ day, commits: [c] })
    }
  }
  return groups
}
