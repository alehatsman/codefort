import { useMemo, useState } from "react"
import { Link, useParams } from "react-router-dom"
import { useMutation, useQueries } from "@tanstack/react-query"
import { api } from "../api/client"
import {
  keys,
  useIntel,
  useIntelOverview,
  useIntelPackageGraph,
  useIntelSummaries,
  useRepo,
} from "../api/queries"
import OverviewCard from "../components/OverviewCard"
import { Button, EmptyState, ErrorMessage, Input, RelativeTime, Spinner } from "../components/ui"
import { absoluteTime, timeAgo } from "../lib/timeAgo"
import type {
  Commit,
  IntelPackageGraph,
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
  const packageGraphQ = useIntelPackageGraph(owner, repo, isIndexed)
  const summariesQ = useIntelSummaries(owner, repo, isIndexed)

  const packages = overviewQ.data?.packages ?? []
  const lastCommitByPath = usePackageRecency(owner, repo, packages, isIndexed)

  if (repoQ.isLoading) return <Spinner />
  if (repoQ.error) return <ErrorMessage error={repoQ.error} />
  if (!repoQ.data) return null

  const r = repoQ.data
  const intel = intelQ.data

  return (
    <div className="explore-page">
      <OverviewCard owner={r.owner} repo={r.name} path="" summaries={{}} />

      {intelQ.isLoading && <Spinner label="Loading index status…" />}
      <ErrorMessage error={intelQ.error} />

      {intel && !intel.enabled && (
        <EmptyState bordered>
          <p>
            <strong>Code intelligence is not configured.</strong>
          </p>
          <p className="muted small">
            Set <code>MOONGIT_DEX_URL</code> (and <code>MOONGIT_DEX_TOKEN</code> if dex requires
            one) to point at a running <code>dex serve</code> daemon, then restart moongitd.
          </p>
        </EmptyState>
      )}

      {intel?.enabled && !intel.found && (
        <EmptyState bordered>
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
        </EmptyState>
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
            graph={packageGraphQ.data}
            graphLoading={packageGraphQ.isLoading}
            lastCommitByPath={lastCommitByPath}
            loading={overviewQ.isLoading}
            error={overviewQ.error as Error | null}
          />
        </div>
      )}

      {/* summariesQ is fetched so the breadcrumb / future panels share its
          cache; surface its error rather than failing silently. */}
      {isIndexed && <ErrorMessage error={summariesQ.error} />}
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
              <RelativeTime className="hotspot__when" iso={commit.date} />
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

interface MapProps {
  owner: string
  repo: string
  packages: IntelPackageSummary[]
  graph?: IntelPackageGraph
  graphLoading: boolean
  lastCommitByPath: Record<string, Commit>
  loading: boolean
  error: Error | null
}

// PackageMap prefers dex's real package import DAG (graph-driven layering +
// cross-links). When dex returns no graph — a non-Go or un-graphed repo — it
// falls back to the path-name grouping so those repos still render a map.
function PackageMap(props: MapProps) {
  const { graph } = props
  // Prefer the graph map only when packages have real import edges (a Go-only
  // DAG today). dex emits a node per non-Go dir too (web/src TS modules,
  // testdata fixtures) but no package-level edges for them — a repo whose
  // nodes are all isolated has no structure to draw, so fall back to the
  // path-name summary listing.
  const hasLinked = !!graph?.nodes.some((n) => n.in_degree > 0 || n.out_degree > 0)
  if (graph && graph.status === "ok" && hasLinked) {
    return <GraphPackageMap {...props} graph={graph} />
  }
  // The graph query hasn't resolved yet (it's slower than the overview on big
  // repos): show the loading state rather than flashing the path-name fallback
  // and then swapping it for the graph map.
  if (!graph && props.graphLoading) {
    return (
      <section className="explore-section">
        <Spinner label="Loading map…" />
      </section>
    )
  }
  return <FallbackPackageMap {...props} />
}

function FallbackPackageMap({ owner, repo, packages, lastCommitByPath, loading, error }: MapProps) {
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

  if (loading) return <Spinner label="Loading map…" />
  if (error) return <ErrorMessage error={error} />
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

/* ── Graph-driven package map ────────────────────────────────────────────
   dex's package import DAG is the real structure: rank by in-degree (how
   load-bearing a package is), layer by import depth (foundation at the
   bottom, entry points on top), and cross-link each card to the internal
   packages it uses (→) and is used by (←). Summaries + git recency from the
   existing joins still ride on each card. */

interface PkgRef {
  label: string
  repoRel: string
  local: boolean // false when we can't map the import path to a repo dir
}

interface PkgCard extends PkgRef {
  pkg: string
  summary: string
  commit?: Commit
  inDegree: number
  outDegree: number
  uses: PkgRef[]
  usedBy: PkgRef[]
}

interface Tier {
  depth: number
  label: string
  cards: PkgCard[]
}

interface MapModel {
  tiers: Tier[]
  linkedCount: number // packages with ≥1 import edge (the ones we draw)
  hiddenCount: number // isolated nodes (non-Go / un-graphed) left out
}

function GraphPackageMap({
  owner,
  repo,
  packages,
  graph,
  lastCommitByPath,
  loading,
  error,
}: MapProps & { graph: IntelPackageGraph }) {
  const { tiers, linkedCount, hiddenCount } = useMemo(
    () => buildTiers(graph, packages, lastCommitByPath),
    [graph, packages, lastCommitByPath]
  )

  if (loading) return <Spinner label="Loading map…" />
  if (error) return <ErrorMessage error={error} />
  if (tiers.length === 0) return null

  return (
    <section className="explore-section">
      <h2 className="explore-section__heading">
        Map of the codebase{" "}
        <span className="muted small">
          ({linkedCount} packages · {graph.edges.length} import edges
          {hiddenCount > 0 ? ` · ${hiddenCount} unlinked hidden` : ""})
        </span>
      </h2>
      <p className="pkg-map__legend muted small">
        Layered by import depth — entry points on top, foundation below. Each card shows how many
        internal packages use it (←) and that it uses (→).
      </p>
      <div className="pkg-map">
        {tiers.map((tier) => (
          <div key={tier.depth} className="pkg-layer">
            <h3 className="pkg-layer__name">
              {tier.label} <span className="muted small">({tier.cards.length})</span>
            </h3>
            <div className="pkg-layer__cards">
              {tier.cards.map((c) => (
                <GraphPackageCard key={c.pkg} owner={owner} repo={repo} card={c} />
              ))}
            </div>
          </div>
        ))}
      </div>
    </section>
  )
}

function GraphPackageCard({ owner, repo, card }: { owner: string; repo: string; card: PkgCard }) {
  return (
    <details className="pkg-card">
      <summary className="pkg-card__summary">
        <span className="pkg-card__path">{card.label}</span>
        <span
          className="pkg-card__degree muted small"
          title={`used by ${card.inDegree} · uses ${card.outDegree} internal packages`}
        >
          ←{card.inDegree} →{card.outDegree}
        </span>
        {card.commit && (
          <RelativeTime className="pkg-card__when muted small" iso={card.commit.date} />
        )}
        {card.summary && (
          <span className="pkg-card__preview muted small">{firstLine(card.summary)}</span>
        )}
      </summary>
      {card.summary && <div className="pkg-card__body">{card.summary}</div>}
      {(card.usedBy.length > 0 || card.uses.length > 0) && (
        <div className="pkg-card__deps">
          {card.usedBy.length > 0 && (
            <div className="pkg-card__deprow">
              <span className="pkg-card__deplabel muted small">used by ←</span>
              {card.usedBy.map((r) => (
                <DepLink key={r.label} owner={owner} repo={repo} dep={r} />
              ))}
            </div>
          )}
          {card.uses.length > 0 && (
            <div className="pkg-card__deprow">
              <span className="pkg-card__deplabel muted small">uses →</span>
              {card.uses.map((r) => (
                <DepLink key={r.label} owner={owner} repo={repo} dep={r} />
              ))}
            </div>
          )}
        </div>
      )}
      {card.local && card.repoRel !== "." && (
        <Link className="pkg-card__browse" to={`/${owner}/${repo}/tree/${card.repoRel}`}>
          Browse files →
        </Link>
      )}
    </details>
  )
}

// DepLink renders a cross-link to another package's files, or plain text when
// the import path couldn't be mapped to a repo directory (mixed-language /
// vendored paths).
function DepLink({ owner, repo, dep }: { owner: string; repo: string; dep: PkgRef }) {
  if (!dep.local) return <span className="pkg-dep pkg-dep--plain">{dep.label}</span>
  return (
    <Link className="pkg-dep" to={`/${owner}/${repo}/tree/${dep.repoRel}`}>
      {dep.label}
    </Link>
  )
}

// buildTiers turns dex's package graph into depth-layered, in-degree-ranked
// cards joined to summaries + recency. Pure so it unit/UI-tests cleanly.
//
// Only packages that participate in the import DAG (in or out degree > 0) are
// drawn: dex's package graph is Go-only, so non-Go dirs (web/src TS modules,
// testdata fixtures) come back as isolated nodes that carry no structural
// signal and would otherwise flood the map. They're counted in hiddenCount.
// Deriving the module prefix from the linked set (not all nodes) also keeps
// one stray fixture from collapsing it to "" and full-pathing every label.
function buildTiers(
  graph: IntelPackageGraph,
  packages: IntelPackageSummary[],
  lastCommitByPath: Record<string, Commit>
): MapModel {
  const linked = graph.nodes.filter((n) => n.in_degree > 0 || n.out_degree > 0)
  const hiddenCount = graph.nodes.length - linked.length
  const prefix = deriveModulePrefix(linked.map((n) => n.package))
  const refOf = (pkg: string): PkgRef => {
    if (prefix && pkg === prefix) return { label: ".", repoRel: ".", local: true }
    if (prefix && pkg.startsWith(`${prefix}/`)) {
      const repoRel = pkg.slice(prefix.length + 1)
      return { label: repoRel, repoRel, local: true }
    }
    // No module-prefix match (mixed-language fixtures, vendored paths): keep
    // the import path as the label and mark it non-navigable.
    return { label: pkg, repoRel: pkg, local: false }
  }

  const summaryByPath = new Map(packages.map((p) => [p.path, p.summary]))
  const uses = new Map<string, string[]>()
  const usedBy = new Map<string, string[]>()
  for (const e of graph.edges) {
    pushTo(uses, e.from_package, e.to_package)
    pushTo(usedBy, e.to_package, e.from_package)
  }

  const depth = computeDepths(linked, uses)
  const maxDepth = linked.reduce((m, n) => Math.max(m, depth.get(n.package) ?? 0), 0)
  const byLabel = (a: PkgRef, b: PkgRef) => a.label.localeCompare(b.label)

  const cards: PkgCard[] = linked.map((n) => {
    const ref = refOf(n.package)
    return {
      ...ref,
      pkg: n.package,
      summary: ref.local ? (summaryByPath.get(ref.repoRel) ?? "") : "",
      commit: ref.local ? lastCommitByPath[ref.repoRel] : undefined,
      inDegree: n.in_degree,
      outDegree: n.out_degree,
      uses: (uses.get(n.package) ?? []).map(refOf).sort(byLabel),
      usedBy: (usedBy.get(n.package) ?? []).map(refOf).sort(byLabel),
    }
  })

  const byDepth = new Map<number, PkgCard[]>()
  for (const c of cards) {
    const d = depth.get(c.pkg) ?? 0
    const list = byDepth.get(d) ?? []
    list.push(c)
    byDepth.set(d, list)
  }
  const tiers = [...byDepth.keys()]
    .sort((a, b) => b - a) // entry points (deep) first, foundation last
    .map((d) => ({
      depth: d,
      label: tierLabel(d, maxDepth),
      cards: (byDepth.get(d) as PkgCard[]).sort(
        (a, b) => b.inDegree - a.inDegree || a.label.localeCompare(b.label)
      ),
    }))
  return { tiers, linkedCount: linked.length, hiddenCount }
}

// computeDepths assigns each package the length of its longest import chain
// down to a leaf — 0 for packages that import no internal package (the
// foundation). Go's import graph is acyclic; the visiting set guards against
// a cycle anyway so a malformed graph can't loop forever.
function computeDepths(
  nodes: IntelPackageGraph["nodes"],
  uses: Map<string, string[]>
): Map<string, number> {
  const depth = new Map<string, number>()
  const visiting = new Set<string>()
  const dfs = (pkg: string): number => {
    const memo = depth.get(pkg)
    if (memo !== undefined) return memo
    if (visiting.has(pkg)) return 0
    visiting.add(pkg)
    let d = 0
    for (const dep of uses.get(pkg) ?? []) d = Math.max(d, 1 + dfs(dep))
    visiting.delete(pkg)
    depth.set(pkg, d)
    return d
  }
  for (const n of nodes) dfs(n.package)
  return depth
}

function tierLabel(depth: number, maxDepth: number): string {
  if (maxDepth === 0) return "Packages" // no internal import edges discovered
  if (depth === maxDepth) return "Entry points"
  if (depth === 0) return "Foundation"
  return `Layer ${depth}`
}

// deriveModulePrefix finds the repo's Go module path among the import paths.
// Grouping by first path segment and taking the common prefix of the LARGEST
// group keeps a stray off-module node (e.g. a python/js testdata fixture whose
// dotted path shares no prefix with the Go packages) from collapsing the
// prefix to "" — which would full-path every label and break navigation.
// Off-module nodes simply don't match the prefix and stay non-navigable.
function deriveModulePrefix(paths: string[]): string {
  if (paths.length === 0) return ""
  const byHead = new Map<string, string[]>()
  for (const p of paths) pushTo(byHead, p.split("/")[0], p)
  let largest: string[] = []
  for (const group of byHead.values()) if (group.length > largest.length) largest = group
  return commonPathPrefix(largest)
}

// commonPathPrefix returns the longest segment-aligned shared prefix of the
// import paths — the Go module path for a single-module repo, used to map an
// import path to its repo-relative directory.
function commonPathPrefix(paths: string[]): string {
  if (paths.length === 0) return ""
  let parts = paths[0].split("/")
  for (const p of paths) {
    const ps = p.split("/")
    let i = 0
    while (i < parts.length && i < ps.length && parts[i] === ps[i]) i++
    parts = parts.slice(0, i)
    if (parts.length === 0) return ""
  }
  return parts.join("/")
}

function pushTo(m: Map<string, string[]>, key: string, val: string) {
  const a = m.get(key)
  if (a) a.push(val)
  else m.set(key, [val])
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
        {commit && <RelativeTime className="pkg-card__when muted small" iso={commit.date} />}
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
          <Input
            className="explore-ask__input"
            placeholder={placeholderFor(kind)}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
          <Button
            type="submit"
            variant="primary"
            className="explore-ask__go"
            disabled={!query.trim() || search.isPending}
          >
            {search.isPending ? "Working…" : verbFor(kind)}
          </Button>
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

      <ErrorMessage error={search.error} />
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
  // dex's /ask returns extra structure (a synthesized answer, next_action,
  // suggested_reads, annotations). Render that CLI-style. Other kinds keep
  // the flat list.
  const isAsk = !!(result.answer || result.next_action || result.suggested_reads?.length)
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
      <EmptyState>
        <p className="muted small">{result.hint || `dex returned: ${result.status}`}</p>
      </EmptyState>
    )
  }
  const reads = result.suggested_reads ?? []
  const ann = result.annotations ?? {}
  return (
    <div className="ask">
      {result.hint && <div className="ask__intent muted small">{result.hint}</div>}
      {result.answer && (
        <div className="ask__answer">
          <p className="ask__answer-body">{result.answer}</p>
          {result.answer_model && (
            <span className="ask__answer-model muted small">— {result.answer_model}</span>
          )}
        </div>
      )}
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

      {!result.answer &&
        reads.length === 0 &&
        result.hits.length === 0 &&
        (!result.graph || result.graph.nodes.length === 0) && <EmptyState>No matches.</EmptyState>}
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
      <EmptyState>
        <p className="muted small">{result.hint || `dex returned: ${result.status}`}</p>
      </EmptyState>
    )
  }
  if (result.hits.length === 0) {
    return <EmptyState>No matches{result.hint ? ` (${result.hint})` : ""}.</EmptyState>
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
