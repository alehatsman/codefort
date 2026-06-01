import { useMemo, useState } from "react"
import { Link, useParams } from "react-router-dom"
import { useMutation, useQueries } from "@tanstack/react-query"
import { api } from "../api/client"
import { keys, useIntel, useIntelOverview, useIntelSummaries, useRepo } from "../api/queries"
import OverviewCard from "../components/OverviewCard"
import { absoluteTime, timeAgo } from "../lib/timeAgo"
import type {
  Commit,
  IntelPackageSummary,
  IntelProject,
  IntelSearchKind,
  IntelSearchResult,
} from "../api/types"

/**
 * Explore tab: the single human-facing "what is this codebase" home, merged
 * from the old Research and Summaries tabs. It leads with dex's *precomputed*
 * understanding — the repo summary, a layered map of packages, and where work
 * is happening — all of which render even when the embedding service is down.
 * The live ask box rides on top as an interactive layer that degrades to an
 * error in place. Package recency (and the hotspots panel) come from
 * tree-commits fetched once per distinct package-parent directory, joined to
 * the dex summaries by path.
 */
export default function ExplorePage() {
  const { owner = "", repo = "" } = useParams()
  const repoQ = useRepo(owner, repo)
  const intelQ = useIntel(owner, repo)
  const isIndexed = !!(intelQ.data?.enabled && intelQ.data?.found)
  const overviewQ = useIntelOverview(owner, repo, isIndexed)
  const summariesQ = useIntelSummaries(owner, repo, isIndexed)

  const packages = overviewQ.data?.packages ?? []
  const lastCommitByPath = usePackageRecency(owner, repo, packages, isIndexed)

  if (repoQ.isLoading) return <div className="loading">Loading…</div>
  if (repoQ.error) return <div className="error">{(repoQ.error as Error).message}</div>
  if (!repoQ.data) return null

  const r = repoQ.data
  const intel = intelQ.data

  return (
    <div className="explore-page">
      <OverviewCard owner={r.owner} repo={r.name} path="" summaries={{}} />

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

      {intel?.enabled && !intel.found && (
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

      {isIndexed && intel?.project && (
        <div className="explore">
          <Hero
            repo={r.name}
            project={intel.project}
            summary={overviewQ.data?.repo_summary}
            loading={overviewQ.isLoading}
          />

          <AskBox owner={r.owner} repo={r.name} />

          <Hotspots
            owner={r.owner}
            repo={r.name}
            packages={packages}
            lastCommitByPath={lastCommitByPath}
          />

          <PackageMap
            owner={r.owner}
            repo={r.name}
            packages={packages}
            lastCommitByPath={lastCommitByPath}
            loading={overviewQ.isLoading}
            error={overviewQ.error as Error | null}
          />
        </div>
      )}

      {/* summariesQ is fetched so the breadcrumb / future panels share its
          cache; surface its error rather than failing silently. */}
      {isIndexed && summariesQ.error && (
        <div className="error">{(summariesQ.error as Error).message}</div>
      )}
    </div>
  )
}

/* ── Package recency ─────────────────────────────────────────────────────
   dex gives us package paths but not their git history. tree-commits returns
   "child full path -> last commit" for one directory, so we fetch it once per
   distinct package-parent directory and merge the entry maps into a single
   path -> last commit lookup. ~4 calls for a typical repo, all cached. */
function usePackageRecency(
  owner: string,
  repo: string,
  packages: IntelPackageSummary[],
  enabled: boolean
): Record<string, Commit> {
  const parents = useMemo(() => distinctParents(packages), [packages])
  const treeQs = useQueries({
    queries: parents.map((parent) => ({
      queryKey: keys.treeCommits(owner, repo, parent, ""),
      queryFn: () => api.getTreeCommits(owner, repo, parent, ""),
      enabled: enabled && !!owner && !!repo,
      staleTime: 60_000,
    })),
  })
  return useMemo(() => {
    const byPath: Record<string, Commit> = {}
    for (const q of treeQs) {
      if (q.data?.entries) Object.assign(byPath, q.data.entries)
    }
    return byPath
  }, [treeQs])
}

function distinctParents(packages: IntelPackageSummary[]): string[] {
  const set = new Set<string>()
  for (const p of packages) {
    if (p.path === "." || p.path === "") continue
    const i = p.path.lastIndexOf("/")
    set.add(i === -1 ? "" : p.path.slice(0, i))
  }
  return [...set]
}

/* ── Hero ────────────────────────────────────────────────────────────────
   The repo summary as the lead answer, with the index's headline numbers as a
   confident chip strip (the dry IndexMeta footer, promoted and trimmed). A
   collapsible <details> card (same pattern as DiffView's file cards): the repo
   name is the always-visible toggle, the prose + stats the body, so a reader
   can close the summary once they've read it. */
function Hero({
  repo,
  project,
  summary,
  loading,
}: {
  repo: string
  project: IntelProject
  summary?: string
  loading: boolean
}) {
  const composed = Math.max(0, project.chunks - project.pending_summaries)
  return (
    <details className="explore-hero" open>
      <summary className="explore-hero__head">
        <h1 className="explore-hero__name">{repo}</h1>
      </summary>
      <div className="explore-hero__body">
        {summary?.trim() ? (
          <p className="explore-hero__summary">{summary.trim()}</p>
        ) : loading ? (
          <p className="muted small">Loading summary…</p>
        ) : (
          <p className="muted small">dex hasn't composed a repo summary yet.</p>
        )}
        <div className="explore-stats">
          <span className="explore-stat">{project.files.toLocaleString()} files</span>
          <span className="explore-stat">{project.chunks.toLocaleString()} chunks</span>
          <span className="explore-stat">{composed.toLocaleString()} summaries</span>
          {project.pending_summaries > 0 && (
            <span className="explore-stat explore-stat--pending">
              {project.pending_summaries.toLocaleString()} pending
            </span>
          )}
          {project.last_indexed && (
            <span className="explore-stat" title={absoluteTime(project.last_indexed)}>
              indexed {timeAgo(project.last_indexed)}
            </span>
          )}
          {project.embed_model && <span className="explore-stat">{project.embed_model}</span>}
        </div>
      </div>
    </details>
  )
}

/* ── Hotspots ──────────────────────────────────────────────────────────────
   "Where work is happening": the packages touched most recently, newest first,
   each annotated with its dex summary. Honest stand-in for "hottest pages" —
   there's no traffic metric, so recency of change is the available signal. */
function Hotspots({
  owner,
  repo,
  packages,
  lastCommitByPath,
}: {
  owner: string
  repo: string
  packages: IntelPackageSummary[]
  lastCommitByPath: Record<string, Commit>
}) {
  const ranked = useMemo(() => {
    return packages
      .filter((p) => p.path !== "." && p.path !== "" && lastCommitByPath[p.path])
      .map((p) => ({ pkg: p, commit: lastCommitByPath[p.path] }))
      .sort((a, b) => Date.parse(b.commit.date) - Date.parse(a.commit.date))
      .slice(0, 6)
  }, [packages, lastCommitByPath])

  if (ranked.length === 0) return null

  return (
    <section className="explore-section">
      <h2 className="explore-section__heading">Where work is happening</h2>
      <ul className="hotspots">
        {ranked.map(({ pkg, commit }) => (
          <li key={pkg.path} className="hotspot">
            <div className="hotspot__head">
              <Link className="hotspot__path" to={`/${owner}/${repo}/tree/${pkg.path}`}>
                {pkg.path}
              </Link>
              <span className="hotspot__when" title={absoluteTime(commit.date)}>
                {timeAgo(commit.date)}
              </span>
            </div>
            <div className="hotspot__subject">{commit.subject}</div>
            <div className="hotspot__summary muted small">{firstLine(pkg.summary)}</div>
          </li>
        ))}
      </ul>
    </section>
  )
}

/* ── Package map ─────────────────────────────────────────────────────────
   The flat alphabetical <details> dump becomes a layered map: packages grouped
   by the role their path implies, each a card with its one-line summary, last
   active time, and the full prose one click away. */
const LAYER_ORDER = [
  "HTTP / API",
  "Storage",
  "Code intel",
  "CI",
  "Web UI",
  "Entry points",
  "Other",
] as const

function layerOf(path: string): (typeof LAYER_ORDER)[number] {
  const p = path.toLowerCase()
  if (p === "web" || p.startsWith("web/") || p.includes("/web/")) return "Web UI"
  if (p === "cmd" || p.startsWith("cmd/")) return "Entry points"
  if (/(^|\/)(server|api|http|handler|router)(\/|$)/.test(p)) return "HTTP / API"
  if (/(^|\/)(storage|store|db|database|sql)(\/|$)/.test(p)) return "Storage"
  if (/(^|\/)(dex|intel|search|index|embed)(\/|$)/.test(p)) return "Code intel"
  if (/(^|\/)(ci|pipeline|runner)(\/|$)/.test(p)) return "CI"
  return "Other"
}

function PackageMap({
  owner,
  repo,
  packages,
  lastCommitByPath,
  loading,
  error,
}: {
  owner: string
  repo: string
  packages: IntelPackageSummary[]
  lastCommitByPath: Record<string, Commit>
  loading: boolean
  error: Error | null
}) {
  const groups = useMemo(() => {
    const byLayer = new Map<string, IntelPackageSummary[]>()
    for (const p of packages) {
      if (p.path === "." || p.path === "") continue
      const layer = layerOf(p.path)
      const list = byLayer.get(layer) ?? []
      list.push(p)
      byLayer.set(layer, list)
    }
    for (const list of byLayer.values()) list.sort((a, b) => a.path.localeCompare(b.path))
    return LAYER_ORDER.filter((l) => byLayer.has(l)).map((l) => ({
      layer: l,
      pkgs: byLayer.get(l) as IntelPackageSummary[],
    }))
  }, [packages])

  if (loading) return <div className="loading">Loading map…</div>
  if (error) return <div className="error">{error.message}</div>
  if (groups.length === 0) return null

  return (
    <section className="explore-section">
      <h2 className="explore-section__heading">
        Map of the codebase <span className="muted small">({packages.length} packages)</span>
      </h2>
      <div className="pkg-map">
        {groups.map(({ layer, pkgs }) => (
          <div key={layer} className="pkg-layer">
            <h3 className="pkg-layer__name">{layer}</h3>
            <div className="pkg-layer__cards">
              {pkgs.map((p) => (
                <PackageCard
                  key={p.path}
                  owner={owner}
                  repo={repo}
                  pkg={p}
                  commit={lastCommitByPath[p.path]}
                />
              ))}
            </div>
          </div>
        ))}
      </div>
    </section>
  )
}

function PackageCard({
  owner,
  repo,
  pkg,
  commit,
}: {
  owner: string
  repo: string
  pkg: IntelPackageSummary
  commit?: Commit
}) {
  return (
    <details className="pkg-card">
      <summary className="pkg-card__summary">
        <span className="pkg-card__path">{pkg.path}</span>
        {commit && (
          <span className="pkg-card__when muted small" title={absoluteTime(commit.date)}>
            {timeAgo(commit.date)}
          </span>
        )}
        <span className="pkg-card__preview muted small">{firstLine(pkg.summary)}</span>
      </summary>
      <div className="pkg-card__body">{pkg.summary}</div>
      <Link className="pkg-card__browse" to={`/${owner}/${repo}/tree/${pkg.path}`}>
        Browse files →
      </Link>
    </details>
  )
}

/* ── Ask box ───────────────────────────────────────────────────────────────
   One natural-language box up front (kind defaults to "ask"). The semantic /
   symbol / callers / callees power modes hide behind an Advanced disclosure so
   newcomers aren't met with a mode picker. */
function AskBox({ owner, repo }: { owner: string; repo: string }) {
  const [query, setQuery] = useState("")
  const [kind, setKind] = useState<IntelSearchKind>("ask")
  const [advanced, setAdvanced] = useState(false)
  const search = useMutation<IntelSearchResult, Error, void>({
    mutationFn: () => api.intelSearch(owner, repo, { query: query.trim(), kind }),
  })

  function submit(e: React.FormEvent) {
    e.preventDefault()
    if (!query.trim() || search.isPending) return
    search.mutate()
  }

  return (
    <section className="explore-ask">
      <form onSubmit={submit}>
        <div className="explore-ask__bar">
          {advanced && (
            <select
              className="input explore-ask__kind"
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
          )}
          <input
            className="input explore-ask__input"
            placeholder={placeholderFor(kind)}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
          <button
            type="submit"
            className="btn btn--primary explore-ask__go"
            disabled={!query.trim() || search.isPending}
          >
            {search.isPending ? "Working…" : verbFor(kind)}
          </button>
        </div>
        <button
          type="button"
          className="explore-ask__advanced"
          aria-expanded={advanced}
          onClick={() => {
            const next = !advanced
            setAdvanced(next)
            if (!next) setKind("ask")
          }}
        >
          {advanced ? "▾" : "▸"} Advanced search modes
        </button>
      </form>

      {search.error && <div className="error">{(search.error as Error).message}</div>}
      {search.data && <IntelResult owner={owner} repo={repo} result={search.data} />}
    </section>
  )
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
  const isAsk = !!(result.next_action || result.suggested_reads?.length)
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
            {reads.map((rd) => {
              const a = ann[rd.path]
              return (
                <li key={`${rd.path}:${rd.start_line}-${rd.end_line}`} className="ask-read">
                  <div className="ask-read__head">
                    <a
                      className="hit__path"
                      href={blobHref(owner, repo, rd.path, rd.start_line, rd.end_line)}
                      target="_blank"
                      rel="noopener noreferrer"
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

      {result.graph && result.graph.nodes.length > 0 && <GraphSection graph={result.graph} />}

      {result.hits.length > 0 && (
        <details className="ask__raw">
          <summary>
            All semantic matches <span className="muted small">({result.hits.length})</span>
          </summary>
          <ul className="intel__hits" style={{ marginTop: 8 }}>
            {result.hits.map((h) => (
              <li key={`${h.path}:${h.start_line}-${h.end_line}`} className="hit">
                <div className="hit__head">
                  <a
                    className="hit__path"
                    href={blobHref(owner, repo, h.path, h.start_line, h.end_line)}
                    target="_blank"
                    rel="noopener noreferrer"
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

      {reads.length === 0 &&
        result.hits.length === 0 &&
        (!result.graph || result.graph.nodes.length === 0) && (
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
    (a, b) =>
      (kindOrder.indexOf(a) === -1 ? 99 : kindOrder.indexOf(a)) -
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
              {groups.get(kind)?.map((n) => (
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
  if (n.qualified_name?.includes("/")) {
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
        {result.hits.map((h) => (
          <li key={`${h.path}:${h.start_line}-${h.end_line}`} className="hit">
            <div className="hit__head">
              <a
                className="hit__path"
                href={blobHref(owner, repo, h.path, h.start_line, h.end_line)}
                target="_blank"
                rel="noopener noreferrer"
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

// Deep link into the blob viewer at the hit's lines. The blob route reads the
// repo-relative path straight from its `*` splat (no ref segment — it resolves
// the default branch), matching how the file tree links files. A multi-line
// span gets a `#L<start>-L<end>` range so CodeView highlights the whole block;
// a single line uses the plain `#L<n>` anchor. Opened in a new tab.
function blobHref(owner: string, repo: string, path: string, start: number, end: number): string {
  const anchor = end > start ? `#L${start}-L${end}` : `#L${start}`
  return `/${owner}/${repo}/blob/${path}${anchor}`
}

function placeholderFor(kind: IntelSearchKind): string {
  switch (kind) {
    case "semantic":
      return "Ask the codebase — e.g. where is auth validated?"
    case "symbol":
      return "Exact identifier — e.g. handleIntelSearch"
    case "ask":
      return "Ask this codebase anything…"
    case "callers":
      return "Symbol whose callers you want — e.g. Open, (*Server).Handler"
    case "callees":
      return "Symbol whose callees you want — e.g. Run, ResolveProject"
  }
}

// The submit button speaks the verb that fits the selected mode, so the
// control reads like an action ("Ask", "Trace") rather than a generic Search.
function verbFor(kind: IntelSearchKind): string {
  switch (kind) {
    case "ask":
      return "Ask"
    case "semantic":
      return "Search"
    case "symbol":
      return "Find"
    case "callers":
    case "callees":
      return "Trace"
  }
}

function firstLine(s: string): string {
  const i = s.indexOf("\n")
  const head = i === -1 ? s : s.slice(0, i)
  return head.length > 140 ? `${head.slice(0, 137)}…` : head
}
