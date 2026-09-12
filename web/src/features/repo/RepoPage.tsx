import { lazy, Suspense } from "react"
import "./repo.css"
import { Navigate, useLocation, useNavigate, useParams, useSearchParams } from "react-router-dom"
import { ApiError } from "@/api/client"
import {
  useBlob,
  useCodeComments,
  useCommitCIStatus,
  useCommits,
  useRepo,
  useTree,
  useTreeCommits,
  useWhoami,
} from "@/api/queries"
import CommitMeta from "@/features/commits/CommitMeta"
import BranchSelector from "@/features/repo/BranchSelector"
import FileTree from "@/features/repo/FileTree"
import LatestCommitBar from "@/features/repo/LatestCommitBar"
import ReadmeCard from "@/features/repo/ReadmeCard"
import { findReadme } from "@/features/repo/readme"
import { useListNav } from "@/shell/keyboardNav"
import NotFound from "@/shell/NotFound"
import OverviewCard from "@/shell/OverviewCard"
import { EmptyState, ErrorMessage, SkeletonText, Spinner } from "@/ui"

// The highlighter grammars are heavy; load them only when a file is viewed.
const CodeView = lazy(() => import("@/features/repo/CodeView"))

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

  if (repoQ.isLoading) return <SkeletonText lines={4} />
  if (repoQ.error) {
    const err = repoQ.error
    if (err instanceof ApiError && err.status === 404) {
      return (
        <NotFound title="Repository not found" detail={`${owner}/${repo} isn’t registered here.`} />
      )
    }
    return <ErrorMessage error={err} />
  }
  if (!repoQ.data) return null

  const r = repoQ.data

  return (
    <div className="repo">
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
  const isRoot = path === ""
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
      void navigate(`/${owner}/${repo}/${kind}/${e.path}`)
    },
  })

  if (treeQ.isLoading) return <SkeletonText heading={false} lines={6} />
  if (treeQ.error) return <ErrorMessage error={treeQ.error} />
  if (!treeQ.data) return null

  // Empty root listing == unborn repo (no commits pushed yet).
  if (isRoot && treeQ.data.entries.length === 0) {
    return (
      <EmptyState bordered>
        <p>
          <strong>This repository is empty.</strong>
        </p>
        <p className="muted small">
          Push your first commit:{" "}
          <code>
            git clone http://localhost:8080/{owner}/{repo}.git
          </code>
        </p>
      </EmptyState>
    )
  }

  const readme = findReadme(treeQ.data.entries)

  return (
    <>
      <OverviewCard owner={owner} repo={repo} path={path} />
      <div className="branch-commit-row">
        <BranchSelector owner={owner} repo={repo} />
        <LatestCommitBar
          owner={owner}
          repo={repo}
          path={path}
          latest={treeCommitsQ.data?.latest}
          ciRun={ciStatusQ.data?.get(treeCommitsQ.data?.latest?.sha ?? "")}
          total={treeCommitsQ.data?.total ?? 0}
          loading={treeCommitsQ.isLoading}
        />
      </div>
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

function BlobView({ owner, repo, path, gitRef, ciEnabled }: ViewProps) {
  const blobQ = useBlob(owner, repo, path, gitRef)
  // Latest commit touching this file, for the GitHub-style header.
  const commitsQ = useCommits(owner, repo, { path, perPage: 1, ref: gitRef })
  const lastCommit = commitsQ.data?.commits?.[0]
  const ciStatusQ = useCommitCIStatus(owner, repo, ciEnabled)
  // Review comments anchored to this file on this branch, plus the viewer's
  // identity for author-only resolve/delete.
  const commentsQ = useCodeComments(owner, repo, { ref: gitRef, path, state: "all" })
  const whoamiQ = useWhoami()

  if (blobQ.isLoading) return <SkeletonText heading={false} lines={8} />
  if (blobQ.error)
    return <BlobError error={blobQ.error} owner={owner} repo={repo} path={path} gitRef={gitRef} />
  if (!blobQ.data) return null

  const b = blobQ.data

  return (
    <>
      <OverviewCard owner={owner} repo={repo} path={path} />
      <BranchSelector owner={owner} repo={repo} />
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
        <BlobBody
          blob={b}
          path={path}
          owner={owner}
          repo={repo}
          comments={commentsQ.data ?? []}
          currentUser={whoamiQ.data?.name}
        />
      </div>
    </>
  )
}

function BlobError({
  error,
  owner,
  repo,
  path,
  gitRef,
}: {
  error: unknown
  owner: string
  repo: string
  path: string
  gitRef: string
}) {
  // A relative link in a rendered README (e.g. `examples/`) can point a
  // /blob/ URL at a directory; the backend 400s with "path is a directory".
  // Send the viewer to the tree view instead of showing a raw error.
  if (error instanceof ApiError && error.status === 400 && /is a directory/.test(error.message)) {
    const suffix = gitRef ? `?ref=${encodeURIComponent(gitRef)}` : ""
    return <Navigate to={`/${owner}/${repo}/tree/${path}${suffix}`} replace />
  }
  return <ErrorMessage error={error} />
}

function BlobBody({
  blob,
  path,
  owner,
  repo,
  comments,
  currentUser,
}: {
  blob: NonNullable<ReturnType<typeof useBlob>["data"]>
  path: string
  owner: string
  repo: string
  comments: NonNullable<ReturnType<typeof useCodeComments>["data"]>
  currentUser: string | undefined
}) {
  if (blob.too_large)
    return <EmptyState>File too large to display ({formatSize(blob.size)}).</EmptyState>
  if (blob.binary) return <EmptyState>Binary file not shown.</EmptyState>
  return (
    <Suspense fallback={<Spinner />}>
      <CodeView
        content={blob.content}
        path={path}
        owner={owner}
        repo={repo}
        codeRef={blob.ref}
        comments={comments}
        currentUser={currentUser}
      />
    </Suspense>
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
