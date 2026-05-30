import PathBreadcrumb from "./PathBreadcrumb"

/**
 * OverviewCard is the single navigation header rendered just below the tabs on
 * every repo route. Its header is the full-path breadcrumb (owner / repo /
 * dir / file). When dex has a summary for the current location, that breadcrumb
 * becomes the header of a collapsible card whose body is the summary (expanded
 * by default), so the AI prose reads as part of the location it describes
 * rather than a separate block. With no summary — every non-Code tab, plus
 * files/dirs dex didn't summarize — it renders the plain breadcrumb alone, so
 * navigation is consistent and never lost.
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
