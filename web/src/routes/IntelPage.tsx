import { useState } from "react"
import { useParams } from "react-router-dom"
import { useMutation } from "@tanstack/react-query"
import { useIntel, useIntelOverview, useRepo } from "../api/queries"
import { api } from "../api/client"
import RepoHeader from "../components/RepoHeader"
import type { IntelOverview, IntelSearchKind, IntelSearchResult } from "../api/types"

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
  const isIndexed = !!(intelQ.data?.enabled && intelQ.data?.found)
  const overviewQ = useIntelOverview(owner, repo, isIndexed)

  const [query, setQuery] = useState("")
  const [kind, setKind] = useState<IntelSearchKind>("ask")
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

          {overviewQ.data && <OverviewSection overview={overviewQ.data} />}

          <form className="intel__search" onSubmit={submit}>
            <select
              className="input intel__kind"
              value={kind}
              onChange={(e) => setKind(e.target.value as IntelSearchKind)}
              aria-label="Search kind"
            >
              <option value="ask">Ask</option>
              <option value="semantic">Semantic</option>
              <option value="symbol">Symbol</option>
              <option value="callers">Callers</option>
              <option value="callees">Callees</option>
            </select>
            <input
              className="input"
              placeholder={placeholderFor(kind)}
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
          {search.data && (
            <IntelResult owner={r.owner} repo={r.name} result={search.data} />
          )}
        </>
      )}
    </div>
  )
}

/**
 * OverviewSection renders the per-package summaries dex composed at index
 * time. The repo-level summary now lives on the Code tab under the tree.
 */
function OverviewSection({ overview }: { overview: IntelOverview }) {
  if (overview.packages.length === 0) {
    return null
  }
  return (
    <section className="overview">
      <h3 className="ask__heading">
        Packages <span className="muted small">({overview.packages.length})</span>
      </h3>
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
  )
}

function firstLine(s: string): string {
  const i = s.indexOf("\n")
  const head = i === -1 ? s : s.slice(0, i)
  return head.length > 120 ? head.slice(0, 117) + "…" : head
}

function IntelResult({
  owner,
  repo,
  result,
}: {
  owner: string
  repo: string
  result: IntelSearchResult
}) {
  // dex's /ask returns extra structure (next_action, suggested_reads,
  // annotations). Render that CLI-style. Other kinds keep the flat list.
  const isAsk = !!(result.next_action || (result.suggested_reads && result.suggested_reads.length))
  if (isAsk) {
    return <AskView owner={owner} repo={repo} result={result} />
  }
  return <SearchHits owner={owner} repo={repo} result={result} />
}

function AskView({
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
  const reads = result.suggested_reads ?? []
  const ann = result.annotations ?? {}
  return (
    <div className="ask">
      {result.hint && <div className="ask__intent muted small">{result.hint}</div>}
      {result.next_action && (
        <div className="ask__next-action">
          <span className="ask__label">Next action</span>
          <p>{result.next_action}</p>
        </div>
      )}
      {result.avoid && (
        <div className="ask__avoid">
          <span className="ask__label">Avoid</span>
          <p>{result.avoid}</p>
        </div>
      )}

      {reads.length > 0 && (
        <>
          <h3 className="ask__heading">
            Suggested reads <span className="muted small">({reads.length})</span>
          </h3>
          <ol className="ask__reads">
            {reads.map((rd, i) => {
              const a = ann[rd.path]
              return (
                <li key={`${rd.path}:${rd.start_line}:${i}`} className="ask-read">
                  <div className="ask-read__head">
                    <a
                      className="hit__path"
                      href={`/${owner}/${repo}/blob/HEAD/${rd.path}#L${rd.start_line}`}
                      onClick={(e) => e.preventDefault()}
                      title="Code browser is on the roadmap"
                    >
                      {rd.path}
                      <span className="hit__lines">
                        :{rd.start_line}-{rd.end_line}
                      </span>
                    </a>
                    {rd.reason && <span className="ask-read__reason">{rd.reason}</span>}
                  </div>
                  {a && (a.nearest_doc || a.tests?.length || a.package) && (
                    <div className="ask-read__chips">
                      {a.package && <span className="ask-chip">pkg: {a.package}</span>}
                      {a.nearest_doc && <span className="ask-chip">doc: {a.nearest_doc}</span>}
                      {a.tests && a.tests.length > 0 && (
                        <span className="ask-chip">tests: {a.tests.join(", ")}</span>
                      )}
                    </div>
                  )}
                  {rd.content && (
                    <pre className="hit__code">
                      {rd.content}
                      {rd.truncated ? "\n…" : ""}
                    </pre>
                  )}
                </li>
              )
            })}
          </ol>
        </>
      )}

      {result.graph && result.graph.nodes.length > 0 && (
        <GraphSection graph={result.graph} />
      )}

      {result.hits.length > 0 && (
        <details className="ask__raw">
          <summary>
            All semantic matches{" "}
            <span className="muted small">({result.hits.length})</span>
          </summary>
          <ul className="intel__hits" style={{ marginTop: 8 }}>
            {result.hits.map((h, i) => (
              <li key={`${h.path}:${h.start_line}:${i}`} className="hit">
                <div className="hit__head">
                  <a
                    className="hit__path"
                    href={`/${owner}/${repo}/blob/HEAD/${h.path}#L${h.start_line}`}
                    onClick={(e) => e.preventDefault()}
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
        </details>
      )}

      {reads.length === 0 && result.hits.length === 0 && (!result.graph || result.graph.nodes.length === 0) && (
        <div className="empty">No matches.</div>
      )}
    </div>
  )
}

function GraphSection({ graph }: { graph: NonNullable<IntelSearchResult["graph"]> }) {
  // Group by kind so the section reads as "Packages: x, y · Functions: a, b".
  const groups = new Map<string, typeof graph.nodes>()
  for (const n of graph.nodes) {
    const k = n.kind || "node"
    const list = groups.get(k) ?? []
    list.push(n)
    groups.set(k, list)
  }
  const kindOrder = ["package", "struct", "interface", "function", "method", "field", "node"]
  const sortedKinds = [...groups.keys()].sort(
    (a, b) => (kindOrder.indexOf(a) === -1 ? 99 : kindOrder.indexOf(a)) -
              (kindOrder.indexOf(b) === -1 ? 99 : kindOrder.indexOf(b))
  )
  return (
    <>
      <h3 className="ask__heading">
        Related symbols{" "}
        <span className="muted small">
          ({graph.nodes.length} nodes, {graph.edges.length} edges)
        </span>
      </h3>
      <div className="ask__graph">
        {sortedKinds.map((kind) => (
          <div key={kind} className="ask-graph-row">
            <span className="ask-graph-kind">{kind}</span>
            <div className="ask-graph-nodes">
              {groups.get(kind)!.map((n) => (
                <span key={n.id} className="ask-chip" title={n.qualified_name || n.id}>
                  {shortName(n)}
                </span>
              ))}
            </div>
          </div>
        ))}
      </div>
    </>
  )
}

function shortName(n: { id: string; qualified_name?: string }): string {
  // For packages dex returns the full module path; show the basename so chips fit.
  if (n.qualified_name && n.qualified_name.includes("/")) {
    return n.qualified_name.split("/").pop() || n.id
  }
  return n.qualified_name || n.id
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
    return <div className="empty">No matches{result.hint ? ` (${result.hint})` : ""}.</div>
  }
  return (
    <>
      {result.hint && <div className="intel__hint muted small">{result.hint}</div>}
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
    </>
  )
}

function placeholderFor(kind: IntelSearchKind): string {
  switch (kind) {
    case "semantic":
      return "Ask the codebase — e.g. where is auth validated?"
    case "symbol":
      return "Exact identifier — e.g. handleIntelSearch"
    case "ask":
      return "Free-form question — dex picks the strategy"
    case "callers":
      return "Symbol whose callers you want — e.g. Open, (*Server).Handler"
    case "callees":
      return "Symbol whose callees you want — e.g. Run, ResolveProject"
  }
}

function formatTime(s: string): string {
  if (!s) return "—"
  const d = new Date(s)
  return Number.isNaN(d.getTime()) ? s : d.toLocaleString()
}
