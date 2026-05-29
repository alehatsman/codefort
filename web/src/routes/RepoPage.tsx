import { lazy, Suspense } from "react"
import { useLocation, useParams } from "react-router-dom"
import { useBlob, useIntel, useIntelOverview, useRepo, useTree } from "../api/queries"
import RepoHeader from "../components/RepoHeader"
import FileTree from "../components/FileTree"
import ReadmeCard from "../components/ReadmeCard"
import PathBreadcrumb from "../components/PathBreadcrumb"
import { findReadme } from "../lib/readme"

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
      <PathBreadcrumb owner={r.owner} repo={r.name} path={path} />
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
  const treeQ = useTree(owner, repo, path)
  // Repo-level overview belongs on the root view only; gate the dex round
  // trip on the repo being indexed so subdirectory listings stay cheap.
  const isRoot = path === ""
  const intelQ = useIntel(owner, repo)
  const isIndexed = !!(intelQ.data?.enabled && intelQ.data?.found)
  const overviewQ = useIntelOverview(owner, repo, isRoot && isIndexed)

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

  return (
    <>
      <FileTree owner={owner} repo={repo} entries={treeQ.data.entries} />
      {readme && <ReadmeCard owner={owner} repo={repo} dirPath={path} entry={readme} />}
      {isRoot && overviewQ.data?.repo_summary && (
        <section className="overview">
          <div className="overview__repo">
            <h3 className="ask__heading">Repository overview</h3>
            <div className="overview__prose">{overviewQ.data.repo_summary}</div>
          </div>
        </section>
      )}
    </>
  )
}

function BlobView({ owner, repo, path }: ViewProps) {
  const blobQ = useBlob(owner, repo, path)

  if (blobQ.isLoading) return <div className="loading">Loading…</div>
  if (blobQ.error) return <div className="error">{(blobQ.error as Error).message}</div>
  if (!blobQ.data) return null

  const b = blobQ.data

  return (
    <div className="blob">
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
