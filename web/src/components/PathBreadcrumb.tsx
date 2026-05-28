import { Link } from "react-router-dom"

interface Props {
  owner: string
  repo: string
  path: string
}

/**
 * Clickable path crumbs for the code browser, e.g. repo / internal / server
 * / tree.go. Every segment but the last links to its directory in the tree
 * view; the last is the current dir (tree) or file (blob) and stays plain.
 */
export default function PathBreadcrumb({ owner, repo, path }: Props) {
  const base = `/${owner}/${repo}`
  const segments = path === "" ? [] : path.split("/")

  return (
    <div className="path-breadcrumb">
      {segments.length === 0 ? (
        <span className="path-breadcrumb__current">{repo}</span>
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
              <span className="path-breadcrumb__current">{seg}</span>
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
