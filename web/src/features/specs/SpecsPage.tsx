import "./specs.css"
import { useParams } from "react-router-dom"
import { useRepo, useSpecsList } from "@/api/queries"
import OverviewCard from "@/shell/OverviewCard"
import { EmptyState, ErrorMessage, Spinner } from "@/ui"
import type { SpecListItem } from "@/api/types"

/**
 * Specs tab: in-repo, human-authored specifications under specs/ — the dual of
 * the dex-derived Explore view ("what the code *is*"). This is the Phase-1
 * shell: it lists the repo's specs with their status, and explains the
 * convention when none exist. The two-pane tree + markdown render + status rail
 * (the real read UX) lands in #209.
 */
export default function SpecsPage() {
  const { owner = "", repo = "" } = useParams()
  const repoQ = useRepo(owner, repo)
  const specsQ = useSpecsList(owner, repo)

  if (repoQ.isLoading) return <Spinner />
  if (repoQ.error) return <ErrorMessage error={repoQ.error} />
  if (!repoQ.data) return null

  const r = repoQ.data
  const specs = specsQ.data?.specs ?? []

  return (
    <div className="specs-page">
      <OverviewCard owner={r.owner} repo={r.name} path="" summaries={{}} />

      {specsQ.isLoading && <Spinner label="Loading specs…" />}
      <ErrorMessage error={specsQ.error} />

      {specsQ.data && specs.length === 0 && <SpecsEmptyState />}

      {specs.length > 0 && (
        <ul className="spec-list">
          {specs.map((s) => (
            <SpecRow key={s.path} spec={s} />
          ))}
        </ul>
      )}
    </div>
  )
}

function SpecRow({ spec }: { spec: SpecListItem }) {
  return (
    <li className="spec-row">
      <span className={`spec-dot spec-dot--${statusKey(spec.status)}`} aria-hidden />
      <span className="spec-row__title">{spec.title}</span>
      <span className="spec-row__path muted small">{spec.path}</span>
      {spec.status && <span className="spec-row__status muted small">{spec.status}</span>}
    </li>
  )
}

function SpecsEmptyState() {
  return (
    <EmptyState bordered>
      <p>
        <strong>No specs yet.</strong>
      </p>
      <p className="muted small">
        Specs are human-authored markdown under <code>specs/</code> describing what the code{" "}
        <em>should</em> do. Add a <code>specs/&lt;name&gt;.md</code> with optional frontmatter (
        <code>id</code>, <code>status</code>, <code>owners</code>, <code>covers</code>) and the
        sections <code>Intent</code> / <code>Behavior</code> / <code>Checklist</code> /{" "}
        <code>Non-goals</code>. See <code>docs/specs.md</code> for the convention.
      </p>
    </EmptyState>
  )
}

// statusKey normalizes a (possibly unknown) status into one of the status-dot
// modifier classes; anything unrecognized renders as the neutral "living" dot.
function statusKey(status?: string): "living" | "draft" | "superseded" {
  if (status === "draft" || status === "superseded") return status
  return "living"
}
