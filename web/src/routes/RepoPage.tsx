import { lazy, Suspense } from "react"
import { useLocation, useNavigate, useParams } from "react-router-dom"
import {
  useBlob,
  useCommits,
  useIntel,
  useIntelFileSummary,
  useIntelOverview,
  useRepo,
  useTree,
  useTreeCommits,
} from "../api/queries"
import RepoHeader from "../components/RepoHeader"
import FileTree from "../components/FileTree"
import LatestCommitBar from "../components/LatestCommitBar"
import CommitMeta from "../components/CommitMeta"
import ReadmeCard from "../components/ReadmeCard"
import OverviewCard, { breadcrumbSummaries } from "../components/OverviewCard"
import { findReadme } from "../lib/readme"
import { useListNav } from "../lib/keyboardNav"

// The highlighter grammars are heavy; load them only when a file is viewed.
const CodeView = lazy(() => import("../components/CodeView"))

/**
 * Code tab: a GitHub-style browser over the repo's default branch. Serves
 * three routes via one component — repo root, a subdirectory (`/tree/*`),
 * and a file (`/blob/*`) — distinguished by the URL. The splat param ("*")
 * carries the path within the repo.
 */
export default function RepoPage() {
  const { owner = "", repo = "" } = useParams()
  const path = useParams()["*"] ?? ""
  const isBlob = useLocation().pathname.includes(`/${owner}/${repo}/blob/`)

  const repoQ = useRepo(owner, repo)

  if (repoQ.isLoading) return <div className="loading">Loading…</div>
  if (repoQ.error) return <div className="error">{(repoQ.error as Error).message}</div>
  if (!repoQ.data) return null

  const r = repoQ.data

  return (
    <div className="repo">
      <RepoHeader owner={r.owner} repo={r.name} openIssues={r.open_issues} />
      {isBlob ? (
        <BlobView owner={r.owner} repo={r.name} path={path} />
      ) : (
        <TreeView owner={r.owner} repo={r.name} path={path} />
      )}
    </div>
  )
}

interface ViewProps {
  owner: string
  repo: string
  path: string
}

function TreeView({ owner, repo, path }: ViewProps) {
  const navigate = useNavigate()
  const treeQ = useTree(owner, repo, path)
  // The dex overview carries both the repo summary and one summary per
  // package (keyed by directory path), so a single cached query per repo
  // (staleTime 5m) serves every folder view — no extra round trip per dir.
  const isRoot = path === ""
  const intelQ = useIntel(owner, repo)
  const isIndexed = !!(intelQ.data?.enabled && intelQ.data?.found)
  const overviewQ = useIntelOverview(owner, repo, isIndexed)
  const treeCommitsQ = useTreeCommits(owner, repo, path)

  // j/k select a file/folder; Enter opens it. h/l are left to useTabNav.
  const entries = treeQ.data?.entries ?? []
  const { index } = useListNav({
    count: entries.length,
    onActivate: (i) => {
      const e = entries[i]
      if (!e) return
      const kind = e.type === "tree" ? "tree" : "blob"
      navigate(`/${owner}/${repo}/${kind}/${e.path}`)
    },
  })

  if (treeQ.isLoading) return <div className="loading">Loading…</div>
  if (treeQ.error) return <div className="error">{(treeQ.error as Error).message}</div>
  if (!treeQ.data) return null

  // Empty root listing == unborn repo (no commits pushed yet).
  if (isRoot && treeQ.data.entries.length === 0) {
    return (
      <div className="empty" style={{ border: "1px solid var(--border)", borderRadius: 6 }}>
        <p>
          <strong>This repository is empty.</strong>
        </p>
        <p className="muted small">
          Push your first commit:{" "}
          <code>
            git clone http://localhost:8080/{owner}/{repo}.git
          </code>
        </p>
      </div>
    )
  }

  const readme = findReadme(treeQ.data.entries)

  // The dex overview carries the repo summary and one entry per package, so a
  // single map makes the repo crumb and every directory crumb up to here
  // hoverable. Paths dex didn't summarize simply stay plain.
  const summaries = breadcrumbSummaries(overviewQ.data)

  return (
    <>
      <OverviewCard owner={owner} repo={repo} path={path} summaries={summaries} />
      <LatestCommitBar
        owner={owner}
        repo={repo}
        path={path}
        latest={treeCommitsQ.data?.latest}
        total={treeCommitsQ.data?.total ?? 0}
        loading={treeCommitsQ.isLoading}
      />
      <FileTree
        owner={owner}
        repo={repo}
        entries={treeQ.data.entries}
        selectedIndex={index}
        commits={treeCommitsQ.data?.entries}
        commitsLoading={treeCommitsQ.isLoading}
      />
      {readme && <ReadmeCard owner={owner} repo={repo} dirPath={path} entry={readme} />}
    </>
  )
}

function BlobView({ owner, repo, path }: ViewProps) {
  const blobQ = useBlob(owner, repo, path)
  // Per-file dex summary, surfaced in the same collapsible card as the tree
  // view's folder/repo summaries. One dex round trip per file, gated on dex
  // being up and this repo indexed; files dex didn't summarize render no
  // card (OverviewCard falls back to a plain breadcrumb).
  const intelQ = useIntel(owner, repo)
  const isIndexed = !!(intelQ.data?.enabled && intelQ.data?.found)
  const summaryQ = useIntelFileSummary(owner, repo, path, isIndexed)
  // The repo + package overview (one cached query per repo) makes the parent
  // dir and repo crumbs hoverable here too, with the file summary filling the
  // leaf segment.
  const overviewQ = useIntelOverview(owner, repo, isIndexed)
  // Latest commit touching this file, for the GitHub-style header.
  const commitsQ = useCommits(owner, repo, { path, perPage: 1 })
  const lastCommit = commitsQ.data?.commits?.[0]

  if (blobQ.isLoading) return <div className="loading">Loading…</div>
  if (blobQ.error) return <div className="error">{(blobQ.error as Error).message}</div>
  if (!blobQ.data) return null

  const b = blobQ.data
  const summaries = breadcrumbSummaries(overviewQ.data, summaryQ.data)

  return (
    <>
      <OverviewCard owner={owner} repo={repo} path={path} summaries={summaries} />
      <div className="blob">
        {lastCommit && (
          <CommitMeta owner={owner} repo={repo} commit={lastCommit} className="blob__commit" />
        )}
        <div className="blob__head">
          <span className="muted small">
            {lineCount(b.content)} lines · {formatSize(b.size)}
          </span>
        </div>
        {b.too_large ? (
          <div className="empty">File too large to display ({formatSize(b.size)}).</div>
        ) : b.binary ? (
          <div className="empty">Binary file not shown.</div>
        ) : (
          <Suspense fallback={<div className="loading">Loading…</div>}>
            <CodeView content={b.content} path={path} />
          </Suspense>
        )}
      </div>
    </>
  )
}

function lineCount(content: string): number {
  if (content === "") return 0
  const body = content.endsWith("\n") ? content.slice(0, -1) : content
  return body.split("\n").length
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}
