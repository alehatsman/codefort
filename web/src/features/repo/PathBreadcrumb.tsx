import { Link } from "react-router-dom"

interface Props {
  owner: string
  repo: string
  path: string
  /** dex summaries keyed by sub-path: "" → repo root, "dir" / "dir/sub" →
   *  directories, and the full file path → the current blob. Any segment with
   *  a matching entry carries it as a hover tooltip and gets the --info
   *  affordance. Omit / leave empty when dex has nothing for this repo. */
  summaries?: Record<string, string>
}

// A segment carrying a dex summary gets the --info modifier on top of its base
// class, which paints the dotted underline + help cursor that signal a hover
// tooltip is available.
function segClass(base: string, hasSummary: boolean) {
  return hasSummary ? `${base} path-breadcrumb__seg--info` : base
}

/**
 * Clickable full-path crumbs, e.g. owner / repo / internal / server / tree.go.
 * The owner links to the repos list and the repo to its root; every path
 * segment but the last links to its directory in the tree view. Every segment
 * dex has prose for — the repo, any directory, the current file — carries that
 * summary as a native title tooltip (available on hover at all times, no space
 * taken) and is marked with a dotted underline so the affordance is visible.
 * This is the single navigation header shown across repo routes.
 */
const PathBreadcrumb = ({ owner, repo, path, summaries }: Props) => {
  const base = `/${owner}/${repo}`
  const segments = path === "" ? [] : path.split("/")
  const repoSummary = summaries?.[""]

  return (
    <div className="path-breadcrumb">
      <Link to="/" className="path-breadcrumb__seg">
        {owner}
      </Link>
      <span className="path-breadcrumb__sep">/</span>
      {segments.length === 0 ? (
        <span
          className={segClass("path-breadcrumb__current", !!repoSummary)}
          title={repoSummary || undefined}
        >
          {repo}
        </span>
      ) : (
        <Link
          to={base}
          className={segClass("path-breadcrumb__seg", !!repoSummary)}
          title={repoSummary || undefined}
        >
          {repo}
        </Link>
      )}
      {segments.map((seg, i) => {
        const last = i === segments.length - 1
        const sub = segments.slice(0, i + 1).join("/")
        const summary = summaries?.[sub]
        return (
          <span key={sub}>
            <span className="path-breadcrumb__sep">/</span>
            {last ? (
              <span
                className={segClass("path-breadcrumb__current", !!summary)}
                title={summary || undefined}
              >
                {seg}
              </span>
            ) : (
              <Link
                to={`${base}/tree/${sub}`}
                className={segClass("path-breadcrumb__seg", !!summary)}
                title={summary || undefined}
              >
                {seg}
              </Link>
            )}
          </span>
        )
      })}
    </div>
  )
}

export default PathBreadcrumb
