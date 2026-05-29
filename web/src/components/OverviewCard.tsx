/**
 * OverviewCard surfaces a dex-composed summary as a collapsible card on the
 * Code tab — the repo-level summary at the repo root, or a directory's
 * package summary when browsing inside one. Expanded by default so the prose
 * is visible at a glance; the user can fold it away to get the tree + README
 * higher on the page. Renders nothing when there's no summary to show, so
 * callers can pass an empty string without guarding.
 */
export default function OverviewCard({
  title,
  path,
  summary,
}: {
  title: string
  path?: string
  summary: string
}) {
  if (!summary) return null
  return (
    <section className="overview">
      <details className="overview-card" open>
        <summary className="overview-card__head">
          <span className="overview-card__title">{title}</span>
          {path && <code className="overview-card__path">{path}</code>}
        </summary>
        <div className="overview-card__prose">{summary}</div>
      </details>
    </section>
  )
}
