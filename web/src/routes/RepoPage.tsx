import { lazy, Suspense } from "react"
import { Navigate, useLocation, useNavigate, useParams, useSearchParams } from "react-router-dom"
import { ApiError } from "../api/client"
import {
  useBlob,
  useCodeComments,
  useCommitCIStatus,
  useCommits,
  useIntel,
  useIntelSummaries,
  useRepo,
  useTree,
  useTreeCommits,
  useWhoami,
} from "../api/queries"
import BranchSelector from "../components/BranchSelector"
import FileTree from "../components/FileTree"
import LatestCommitBar from "../components/LatestCommitBar"
import CommitMeta from "../components/CommitMeta"
import ReadmeCard from "../components/ReadmeCard"
import OverviewCard from "../components/OverviewCard"
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
  const [params] = useSearchParams()
  const gitRef = params.get("ref") ?? ""

  const repoQ = useRepo(owner, repo)

  if (repoQ.isLoading) return <div className="loading">Loading…</div>
  if (repoQ.error) return <div className="error">{(repoQ.error as Error).message}</div>
  if (!repoQ.data) return null

  const r = repoQ.data

  return (
    <div className="repo">
      <div className="repo-toolbar">
        <BranchSelector owner={r.owner} repo={r.name} />
      </div>
      {isBlob ? (
        <BlobView
          owner={r.owner}
          repo={r.name}
          path={path}
          gitRef={gitRef}
          ciEnabled={r.ci_enabled}
        />
      ) : (
        <TreeView
          owner={r.owner}
          repo={r.name}
          path={path}
          gitRef={gitRef}
          ciEnabled={r.ci_enabled}
        />
      )}
    </div>
  )
}

interface ViewProps {
  owner: string
  repo: string
  path: string
  gitRef: string
  ciEnabled: boolean
}

function TreeView({ owner, repo, path, gitRef, ciEnabled }: ViewProps) {
  const navigate = useNavigate()
  const treeQ = useTree(owner, repo, path, gitRef)
  // Every dex summary for the repo (one cached map): the breadcrumb reads the
  // ancestor sub-paths, the file tree reads each entry.
  const isRoot = path === ""
  const intelQ = useIntel(owner, repo)
  const isIndexed = !!(intelQ.data?.enabled && intelQ.data?.found)
  const summariesQ = useIntelSummaries(owner, repo, isIndexed)
  const treeCommitsQ = useTreeCommits(owner, repo, path, gitRef)
  const ciStatusQ = useCommitCIStatus(owner, repo, ciEnabled)

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

  const summaries = summariesQ.data?.summaries ?? {}

  return (
    <>
      <OverviewCard owner={owner} repo={repo} path={path} summaries={summaries} />
      <LatestCommitBar
        owner={owner}
        repo={repo}
        path={path}
        latest={treeCommitsQ.data?.latest}
        ciRun={ciStatusQ.data?.get(treeCommitsQ.data?.latest?.sha ?? "")}
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
        summaries={summaries}
      />
      {readme && <ReadmeCard owner={owner} repo={repo} dirPath={path} entry={readme} />}
    </>
  )
}

function BlobView({ owner, repo, path, gitRef, ciEnabled }: ViewProps) {
  const blobQ = useBlob(owner, repo, path, gitRef)
  // The repo's full summary map (one cached query); the breadcrumb reads the
  // repo + ancestor dirs + this file from it. Crumbs dex has nothing for stay
  // plain.
  const intelQ = useIntel(owner, repo)
  const isIndexed = !!(intelQ.data?.enabled && intelQ.data?.found)
  const summariesQ = useIntelSummaries(owner, repo, isIndexed)
  // Latest commit touching this file, for the GitHub-style header.
  const commitsQ = useCommits(owner, repo, { path, perPage: 1, ref: gitRef })
  const lastCommit = commitsQ.data?.commits?.[0]
  const ciStatusQ = useCommitCIStatus(owner, repo, ciEnabled)
  // Review comments anchored to this file on this branch, plus the viewer's
  // identity for author-only resolve/delete.
  const commentsQ = useCodeComments(owner, repo, { ref: gitRef, path, state: "all" })
  const whoamiQ = useWhoami()

  if (blobQ.isLoading) return <div className="loading">Loading…</div>
  if (blobQ.error) {
    const err = blobQ.error
    // A relative link in a rendered README (e.g. `examples/`) can point a
    // /blob/ URL at a directory; the backend 400s with "path is a directory".
    // Send the viewer to the tree view instead of showing a raw error.
    if (err instanceof ApiError && err.status === 400 && /is a directory/.test(err.message)) {
      const suffix = gitRef ? `?ref=${encodeURIComponent(gitRef)}` : ""
      return <Navigate to={`/${owner}/${repo}/tree/${path}${suffix}`} replace />
    }
    return <div className="error">{(err as Error).message}</div>
  }
  if (!blobQ.data) return null

  const b = blobQ.data
  const summaries = summariesQ.data?.summaries ?? {}

  return (
    <>
      <OverviewCard owner={owner} repo={repo} path={path} summaries={summaries} />
      <div className="blob">
        {lastCommit && (
          <CommitMeta
            owner={owner}
            repo={repo}
            commit={lastCommit}
            ciRun={ciStatusQ.data?.get(lastCommit.sha)}
            className="blob__commit"
          />
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
            <CodeView
              content={b.content}
              path={path}
              owner={owner}
              repo={repo}
              codeRef={b.ref}
              comments={commentsQ.data ?? []}
              currentUser={whoamiQ.data?.name}
            />
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
