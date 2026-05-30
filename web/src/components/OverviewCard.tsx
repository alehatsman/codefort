import PathBreadcrumb from "./PathBreadcrumb"

/**
 * OverviewCard is the single navigation header rendered just below the tabs on
 * every repo route: the full-path breadcrumb (owner / repo / dir / file),
 * always plain and minimal. dex summaries ride along as hover tooltips on every
 * segment dex has prose for — non-invasive AI prose that takes no space and
 * never breaks the breadcrumb's simplicity, with a dotted underline marking
 * which crumbs are hoverable. No summaries (every non-Code tab, plus paths dex
 * didn't summarize) just renders the breadcrumb, so navigation is identical
 * everywhere.
 */
export default function OverviewCard({
  owner,
  repo,
  path,
  summaries,
}: {
  owner: string
  repo: string
  path: string
  summaries: Record<string, string>
}) {
  return (
    <section className="overview">
      <PathBreadcrumb owner={owner} repo={repo} path={path} summaries={summaries} />
    </section>
  )
}
