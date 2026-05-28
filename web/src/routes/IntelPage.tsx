import { useState } from "react"
import { useParams } from "react-router-dom"
import { useMutation } from "@tanstack/react-query"
import { useIntel, useRepo } from "../api/queries"
import { api } from "../api/client"
import RepoHeader from "../components/RepoHeader"
import type { IntelSearchKind, IntelSearchResult } from "../api/types"

/**
 * Intel tab: surfaces code intelligence from a dex `serve` daemon for this
 * repo — index status plus semantic and symbol search. The backend matches
 * the repo to a dex project by name; when dex isn't configured or the repo
 * isn't indexed, we render a distinct empty state rather than an error.
 */
export default function IntelPage() {
  const { owner = "", repo = "" } = useParams()
  const repoQ = useRepo(owner, repo)
  const intelQ = useIntel(owner, repo)

  const [query, setQuery] = useState("")
  const [kind, setKind] = useState<IntelSearchKind>("semantic")
  const search = useMutation<IntelSearchResult, Error, void>({
    mutationFn: () => api.intelSearch(owner, repo, { query: query.trim(), kind }),
  })

  if (repoQ.isLoading) return <div className="loading">Loading…</div>
  if (repoQ.error) return <div className="error">{(repoQ.error as Error).message}</div>
  if (!repoQ.data) return null

  const r = repoQ.data
  const intel = intelQ.data

  function submit(e: React.FormEvent) {
    e.preventDefault()
    if (!query.trim() || search.isPending) return
    search.mutate()
  }

  return (
    <div className="intel">
      <RepoHeader owner={r.owner} repo={r.name} openIssues={r.open_issues} />

      {intelQ.isLoading && <div className="loading">Loading index status…</div>}
      {intelQ.error && <div className="error">{(intelQ.error as Error).message}</div>}

      {intel && !intel.enabled && (
        <div className="empty" style={{ border: "1px solid var(--border)", borderRadius: 6 }}>
          <p>
            <strong>Code intelligence is not configured.</strong>
          </p>
          <p className="muted small">
            Set <code>MOONGIT_DEX_URL</code> (and <code>MOONGIT_DEX_TOKEN</code> if dex requires
            one) to point at a running <code>dex serve</code> daemon, then restart moongitd.
          </p>
        </div>
      )}

      {intel && intel.enabled && !intel.found && (
        <div className="empty" style={{ border: "1px solid var(--border)", borderRadius: 6 }}>
          <p>
            <strong>This repo is not indexed by dex.</strong>
          </p>
          <p className="muted small">
            Run <code>dex index /path/to/{r.name}</code> and serve it with{" "}
            <code>dex serve --project /path/to/{r.name}</code>.
          </p>
          {intel.service && (
            <p className="muted small">
              dex {intel.service.version} at {intel.service.endpoint} —{" "}
              {intel.service.reachable ? "embeddings reachable" : "embeddings unreachable"}
            </p>
          )}
        </div>
      )}

      {intel && intel.enabled && intel.found && intel.project && (
        <>
          <div className="intel__stats">
            <div className="stat">
              <span className="stat__num">{intel.project.files.toLocaleString()}</span>
              <span className="stat__label">files</span>
            </div>
            <div className="stat">
              <span className="stat__num">{intel.project.chunks.toLocaleString()}</span>
              <span className="stat__label">chunks</span>
            </div>
            <div className="stat">
              <span className="stat__num">{intel.project.dim}</span>
              <span className="stat__label">dimensions</span>
            </div>
            <div className="stat">
              <span className="stat__num">{intel.project.pending_summaries}</span>
              <span className="stat__label">pending summaries</span>
            </div>
          </div>

          <dl className="intel__meta">
            <dt>Model</dt>
            <dd>{intel.project.embed_model || "—"}</dd>
            <dt>Last indexed</dt>
            <dd>{formatTime(intel.project.last_indexed)}</dd>
            <dt>Root</dt>
            <dd>
              <code>{intel.project.root}</code>
            </dd>
          </dl>

          <form className="intel__search" onSubmit={submit}>
            <select
              className="input intel__kind"
              value={kind}
              onChange={(e) => setKind(e.target.value as IntelSearchKind)}
              aria-label="Search kind"
            >
              <option value="semantic">Semantic</option>
              <option value="symbol">Symbol</option>
            </select>
            <input
              className="input"
              placeholder={
                kind === "semantic"
                  ? "Ask the codebase — e.g. where is auth validated?"
                  : "Exact identifier — e.g. handleIntelSearch"
              }
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
            <button
              type="submit"
              className="btn btn--primary"
              disabled={!query.trim() || search.isPending}
            >
              {search.isPending ? "Searching…" : "Search"}
            </button>
          </form>

          {search.error && <div className="error">{(search.error as Error).message}</div>}
          {search.data && <SearchHits owner={r.owner} repo={r.name} result={search.data} />}
        </>
      )}
    </div>
  )
}

function SearchHits({
  owner,
  repo,
  result,
}: {
  owner: string
  repo: string
  result: IntelSearchResult
}) {
  if (result.status !== "ok") {
    return (
      <div className="empty">
        <p className="muted small">{result.hint || `dex returned: ${result.status}`}</p>
      </div>
    )
  }
  if (result.hits.length === 0) {
    return <div className="empty">No matches.</div>
  }
  return (
    <ul className="intel__hits">
      {result.hits.map((h, i) => (
        <li key={`${h.path}:${h.start_line}:${i}`} className="hit">
          <div className="hit__head">
            <a
              className="hit__path"
              href={`/${owner}/${repo}/blob/HEAD/${h.path}#L${h.start_line}`}
              onClick={(e) => e.preventDefault()}
              title="Code browser is on the roadmap"
            >
              {h.path}
              <span className="hit__lines">
                :{h.start_line}-{h.end_line}
              </span>
            </a>
            <span className="hit__meta">
              {h.kind}
              {h.role ? ` · ${h.role}` : ""} · {h.score.toFixed(3)}
            </span>
          </div>
          {h.content && <pre className="hit__code">{h.content}</pre>}
        </li>
      ))}
    </ul>
  )
}

function formatTime(s: string): string {
  if (!s) return "—"
  const d = new Date(s)
  return Number.isNaN(d.getTime()) ? s : d.toLocaleString()
}
