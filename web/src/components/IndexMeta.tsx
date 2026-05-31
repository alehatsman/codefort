import type { IntelProject } from "../api/types"

/**
 * IndexMeta is the de-emphasized footer shared by the Research and Summaries
 * tabs: the index's dry numbers and provenance (files / chunks / dimensions /
 * summaries, the embedding model, when it was last indexed, the on-disk root).
 * It's one small muted strip at the bottom — there when you want it, out of the
 * way when you don't.
 *
 * dex reports the backlog (`pending_summaries`), so the count of composed
 * summaries is derived: summarized chunks = chunks − pending. Both the Research
 * and Summaries tabs show the same footer, including the pending-summaries
 * count when there's a backlog.
 */
export default function IndexMeta({ project }: { project: IntelProject }) {
  const summaries = Math.max(0, project.chunks - project.pending_summaries)
  const stats = [
    `${project.files.toLocaleString()} files`,
    `${project.chunks.toLocaleString()} chunks`,
    `${project.dim} dimensions`,
    `${summaries.toLocaleString()} summaries`,
  ]
  if (project.pending_summaries > 0) {
    stats.push(`${project.pending_summaries.toLocaleString()} pending summaries`)
  }
  return (
    <footer className="research-index muted small">
      <span className="research-index__stats">{stats.join(" · ")}</span>
      <span className="research-index__provenance">
        {project.embed_model || "—"} · indexed {formatTime(project.last_indexed)}
        {" · "}
        <code className="research-index__root">{project.root}</code>
      </span>
    </footer>
  )
}

function formatTime(s: string): string {
  if (!s) return "—"
  const d = new Date(s)
  return Number.isNaN(d.getTime()) ? s : d.toLocaleString()
}
