import { Link } from "react-router-dom"

interface Props {
  owner: string
  repo: string
  path: string
  /** dex summary for the current location, surfaced as a hover tooltip on the
   *  current (last) segment. Omitted when dex has nothing for this path. */
  title?: string
}

/**
 * Clickable full-path crumbs, e.g. owner / repo / internal / server / tree.go.
 * The owner links to the repos list and the repo to its root; every path
 * segment but the last links to its directory in the tree view. The last
 * segment — current dir (tree) or file (blob), or the repo itself at the root —
 * stays plain, and carries the dex summary (when present) as a native title
 * tooltip so the AI prose is available on hover without taking any space. This
 * is the single navigation header shown across repo routes.
 */
export default function PathBreadcrumb({ owner, repo, path, title }: Props) {
  const base = `/${owner}/${repo}`
  const segments = path === "" ? [] : path.split("/")

  return (
    <div className="path-breadcrumb">
      <Link to="/" className="path-breadcrumb__seg">
        {owner}
      </Link>
      <span className="path-breadcrumb__sep">/</span>
      {segments.length === 0 ? (
        <span className="path-breadcrumb__current" title={title || undefined}>
          {repo}
        </span>
      ) : (
        <Link to={base} className="path-breadcrumb__seg">
          {repo}
        </Link>
      )}
      {segments.map((seg, i) => {
        const last = i === segments.length - 1
        const sub = segments.slice(0, i + 1).join("/")
        return (
          <span key={sub}>
            <span className="path-breadcrumb__sep">/</span>
            {last ? (
              <span className="path-breadcrumb__current" title={title || undefined}>
                {seg}
              </span>
            ) : (
              <Link to={`${base}/tree/${sub}`} className="path-breadcrumb__seg">
                {seg}
              </Link>
            )}
          </span>
        )
      })}
    </div>
  )
}
