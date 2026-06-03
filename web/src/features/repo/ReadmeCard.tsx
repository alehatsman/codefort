import { lazy, Suspense } from "react"
import { useBlob } from "@/api/queries"
import { isMarkdown } from "@/features/repo/readme"
import type { TreeEntry } from "@/api/types"
import FileIcon from "@/features/repo/FileIcon"

// The markdown renderer pulls in remark/rehype + the highlighter; load it only
// when a README is actually shown.
const Markdown = lazy(() => import("@/shell/Markdown"))

interface Props {
  owner: string
  repo: string
  /** Directory being viewed; the README's relative links/images resolve here. */
  dirPath: string
  entry: TreeEntry
}

/**
 * GitHub-style README rendered below the file listing. Markdown variants go
 * through the sanitising renderer; other recognised READMEs (.txt, plain
 * README) render verbatim. Binary or oversized blobs are skipped silently.
 */
const ReadmeCard = ({ owner, repo, dirPath, entry }: Props) => {
  const blobQ = useBlob(owner, repo, entry.path)
  if (!blobQ.data) return null
  const b = blobQ.data
  if (b.binary || b.too_large) return null

  return (
    <div className="readme-card">
      <div className="readme-card__head">
        <FileIcon type="blob" />
        <span>{entry.name}</span>
      </div>
      {isMarkdown(entry.name) ? (
        <Suspense fallback={<div className="markdown-body loading">Loading…</div>}>
          <Markdown content={b.content} owner={owner} repo={repo} basePath={dirPath} />
        </Suspense>
      ) : (
        <div className="markdown-body">
          <pre>
            <code>{b.content}</code>
          </pre>
        </div>
      )}
    </div>
  )
}

export default ReadmeCard
