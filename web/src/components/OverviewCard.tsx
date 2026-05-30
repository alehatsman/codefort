import PathBreadcrumb from "./PathBreadcrumb"

/**
 * OverviewCard is the navigation + summary block atop the Code tab's tree
 * view. Its header is the path breadcrumb — the top-bar navigation. When dex
 * has a summary for the current location, that breadcrumb becomes the header
 * of a collapsible card whose body is the summary (expanded by default), so
 * the AI prose reads as part of the location it describes rather than a
 * separate block. With no summary it falls back to a plain breadcrumb, so
 * navigation is never lost.
 */
export default function OverviewCard({
  owner,
  repo,
  path,
  summary,
}: {
  owner: string
  repo: string
  path: string
  summary: string
}) {
  if (!summary) return <PathBreadcrumb owner={owner} repo={repo} path={path} />
  return (
    <section className="overview">
      <details className="overview-card" open>
        <summary className="overview-card__head">
          <PathBreadcrumb owner={owner} repo={repo} path={path} />
        </summary>
        <div className="overview-card__prose">{summary}</div>
      </details>
    </section>
  )
}
