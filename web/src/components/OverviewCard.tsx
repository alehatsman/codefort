import PathBreadcrumb from "./PathBreadcrumb"

/**
 * OverviewCard is the single navigation header rendered just below the tabs on
 * every repo route: the full-path breadcrumb (owner / repo / dir / file),
 * always plain and minimal. When dex has a summary for the current location it
 * rides along as a hover tooltip on the current segment — non-invasive AI prose
 * that takes no space and never breaks the breadcrumb's simplicity. No summary
 * (every non-Code tab, plus files/dirs dex didn't summarize) just renders the
 * breadcrumb, so navigation is identical everywhere.
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
  return (
    <section className="overview">
      <PathBreadcrumb owner={owner} repo={repo} path={path} title={summary} />
    </section>
  )
}
