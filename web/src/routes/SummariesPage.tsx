import { useParams } from "react-router-dom"
import { useIntel, useIntelOverview, useRepo } from "../api/queries"
import RepoHeader from "../components/RepoHeader"
import OverviewCard from "../components/OverviewCard"
import type { IntelOverview } from "../api/types"

/**
 * Summaries tab: the dex-composed prose understanding of the repo — a
 * repo-level overview followed by one collapsible entry per package. This is
 * the read-only "what is this codebase" view, split out from Research so that
 * tab can stay focused on asking questions. Shares Research's dex-status empty
 * states (not configured / not indexed).
 */
export default function SummariesPage() {
  const { owner = "", repo = "" } = useParams()
  const repoQ = useRepo(owner, repo)
  const intelQ = useIntel(owner, repo)
  const isIndexed = !!(intelQ.data?.enabled && intelQ.data?.found)
  const overviewQ = useIntelOverview(owner, repo, isIndexed)

  if (repoQ.isLoading) return <div className="loading">Loading…</div>
  if (repoQ.error) return <div className="error">{(repoQ.error as Error).message}</div>
  if (!repoQ.data) return null

  const r = repoQ.data
  const intel = intelQ.data

  return (
    <div className="summaries-page">
      <RepoHeader owner={r.owner} repo={r.name} openIssues={r.open_issues} />
      <OverviewCard owner={r.owner} repo={r.name} path="" summaries={{}} />

      {intelQ.isLoading && <div className="loading">Loading index status…</div>}
      {intelQ.error && <div className="error">{(intelQ.error as Error).message}</div>}

      {intel && !intel.enabled && (
        <div className="empty" style={{ border: "1px solid var(--border)", borderRadius: 6 }}>
          <p>
            <strong>Code intelligence is not configured.</strong>
          </p>
          <p className="muted small">
            Set <code>MOONGIT_DEX_URL</code> to point at a running <code>dex serve</code> daemon,
            then restart moongitd.
          </p>
        </div>
      )}

      {intel && intel.enabled && !intel.found && (
        <div className="empty" style={{ border: "1px solid var(--border)", borderRadius: 6 }}>
          <p>
            <strong>This repo is not indexed by dex.</strong>
          </p>
          <p className="muted small">
            Run <code>dex index /path/to/{r.name}</code> to generate summaries.
          </p>
        </div>
      )}

      {isIndexed && overviewQ.isLoading && (
        <div className="loading">Loading summaries…</div>
      )}
      {isIndexed && overviewQ.error && (
        <div className="error">{(overviewQ.error as Error).message}</div>
      )}
      {isIndexed && overviewQ.data && <Summaries overview={overviewQ.data} />}
    </div>
  )
}

function Summaries({ overview }: { overview: IntelOverview }) {
  const repoSummary = overview.repo_summary?.trim()
  if (!repoSummary && overview.packages.length === 0) {
    return <div className="empty">dex hasn't composed any summaries for this repo yet.</div>
  }
  return (
    <div className="summaries">
      {repoSummary && (
        <section className="summaries__repo">
          <h2 className="summaries__heading">Overview</h2>
          <div className="summaries__prose">{repoSummary}</div>
        </section>
      )}

      {overview.packages.length > 0 && (
        <section className="overview">
          <h2 className="summaries__heading">
            Packages <span className="muted small">({overview.packages.length})</span>
          </h2>
          <ul className="overview__pkgs">
            {overview.packages.map((p) => (
              <li key={p.path} className="overview-pkg">
                <details>
                  <summary>
                    <code className="overview-pkg__path">{p.path}</code>
                    <span className="overview-pkg__preview muted small">
                      {firstLine(p.summary)}
                    </span>
                  </summary>
                  <div className="overview-pkg__body">{p.summary}</div>
                </details>
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  )
}

function firstLine(s: string): string {
  const i = s.indexOf("\n")
  const head = i === -1 ? s : s.slice(0, i)
  return head.length > 120 ? head.slice(0, 117) + "…" : head
}
