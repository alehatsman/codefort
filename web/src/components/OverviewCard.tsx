import type { IntelFileSummary, IntelOverview } from "../api/types"
import PathBreadcrumb from "./PathBreadcrumb"

/**
 * Builds the per-segment summary map the breadcrumb consumes, keyed by sub-path
 * ("" = repo root, directory paths for folders, the full file path for the
 * current blob). The dex overview already carries the repo summary and one
 * entry per package (keyed by directory path), so every crumb up to the current
 * location can be made hoverable from a single cached query; the optional file
 * summary fills in the leaf on the blob view. Empty summaries are dropped so a
 * segment without prose stays plain.
 */
export function breadcrumbSummaries(
  overview: IntelOverview | undefined,
  fileSummary?: IntelFileSummary,
): Record<string, string> {
  const map: Record<string, string> = {}
  if (overview?.repo_summary) map[""] = overview.repo_summary
  for (const p of overview?.packages ?? []) {
    if (p.summary) map[p.path] = p.summary
  }
  if (fileSummary?.summary) map[fileSummary.path] = fileSummary.summary
  return map
}

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
