import PathBreadcrumb from "@/features/repo/PathBreadcrumb"

/**
 * OverviewCard is the single navigation header rendered just below the tabs on
 * every repo route: the full-path breadcrumb (owner / repo / dir / file),
 * always plain and minimal. Navigation is identical on every route, which is
 * the point — the header is orientation, not a surface for per-route extras.
 */
export default function OverviewCard({
  owner,
  repo,
  path,
}: {
  owner: string
  repo: string
  path: string
}) {
  return (
    <section className="overview">
      <PathBreadcrumb owner={owner} repo={repo} path={path} />
    </section>
  )
}
