import { useEffect, useState } from "react"
import ReactMarkdown, { type Components } from "react-markdown"
import remarkGfm from "remark-gfm"
import rehypeSanitize from "rehype-sanitize"
import { Link } from "react-router-dom"
import { api } from "../api/client"
import { highlight, highlightNodes } from "../lib/highlight"
import { isExternalRef, resolveRepoPath } from "../lib/repoPath"

interface Props {
  content: string
  owner: string
  repo: string
  /** Directory the markdown file lives in ("" at repo root). Anchors relative
   *  links and images. */
  basePath: string
}

/**
 * GitHub-flavored markdown renderer. Output is sanitised (rehype-sanitize's
 * default GitHub schema; no raw HTML pass-through) before our component
 * overrides run, so untrusted README content can't inject script:
 *  - links: relative → in-app route, in-page anchors stay, external → new tab
 *  - images: relative → authenticated raw fetch (see RawImage)
 *  - code fences: highlighted via the shared lowlight instance
 */
export default function Markdown({ content, owner, repo, basePath }: Props) {
  const components: Components = {
    a({ href, children, ...props }) {
      if (!href) return <a {...props}>{children}</a>
      if (href.startsWith("#")) {
        return (
          <a href={href} {...props}>
            {children}
          </a>
        )
      }
      if (isExternalRef(href)) {
        return (
          <a href={href} target="_blank" rel="noopener noreferrer" {...props}>
            {children}
          </a>
        )
      }
      // A root-absolute href is an in-app route (e.g. a run link in an agent's
      // handoff comment, /owner/repo/pipelines/N) — link to it as-is rather
      // than rewriting it into a repo file path under /blob.
      if (href.startsWith("/")) {
        return <Link to={href}>{children}</Link>
      }
      return (
        <Link to={`/${owner}/${repo}/blob/${resolveRepoPath(basePath, href)}`}>{children}</Link>
      )
    },
    img({ src, alt }) {
      if (typeof src !== "string" || isExternalRef(src) || src.startsWith("data:")) {
        return <img src={typeof src === "string" ? src : undefined} alt={alt} />
      }
      return <RawImage owner={owner} repo={repo} path={resolveRepoPath(basePath, src)} alt={alt} />
    },
    code({ className, children }) {
      const match = /language-(\w+)/.exec(className ?? "")
      if (!match) return <code className="md-code-inline">{children}</code>
      const text = String(children).replace(/\n$/, "")
      return <code className="hljs">{highlightNodes(highlight(text, match[1]))}</code>
    },
  }

  return (
    <div className="markdown-body hljs">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[rehypeSanitize]}
        components={components}
      >
        {content}
      </ReactMarkdown>
    </div>
  )
}

interface RawImageProps {
  owner: string
  repo: string
  path: string
  alt?: string
}

/**
 * An <img> whose bytes come from the Bearer-authed /raw endpoint. We fetch the
 * blob, render it via an object URL, and revoke the URL on unmount so blobs
 * don't leak. Falls back to the alt text if the fetch fails (missing file,
 * too large, …).
 */
function RawImage({ owner, repo, path, alt }: RawImageProps) {
  const [url, setUrl] = useState<string>()
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    let cancelled = false
    let objectUrl: string | undefined
    setUrl(undefined)
    setFailed(false)
    api
      .getRawBlob(owner, repo, path)
      .then((blob) => {
        if (cancelled) return
        objectUrl = URL.createObjectURL(blob)
        setUrl(objectUrl)
      })
      .catch(() => {
        if (!cancelled) setFailed(true)
      })
    return () => {
      cancelled = true
      if (objectUrl) URL.revokeObjectURL(objectUrl)
    }
  }, [owner, repo, path])

  if (failed) return <span className="md-img-missing">{alt || path}</span>
  if (!url) return <span className="md-img-loading" role="img" aria-label={alt} />
  return <img src={url} alt={alt} />
}
