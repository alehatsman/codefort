import "./specs.css"
import { useEffect, useMemo, useRef } from "react"
import { useParams, useSearchParams } from "react-router-dom"
import { useRepo, useSpec, useSpecsList } from "@/api/queries"
import Markdown from "@/shell/Markdown"
import OverviewCard from "@/shell/OverviewCard"
import { EmptyState, ErrorMessage, RelativeTime, Spinner } from "@/ui"
import type { SpecContent, SpecListItem } from "@/api/types"

/**
 * Specs tab: in-repo, human-authored specifications under specs/ — the dual of
 * the dex-derived Explore view ("what the code *is*"). A two-pane reader: a
 * folder-grouped spec tree with a status rail on the left, the selected spec
 * rendered as markdown on the right. ↑/↓ (or j/k) move between specs; the
 * selection lives in ?path= so it deep-links and survives a refresh.
 *
 * Drift/verification status (#218+) isn't computed yet, so the rail counts the
 * frontmatter lifecycle states (living / draft / superseded) we actually have.
 */
export default function SpecsPage() {
  const { owner = "", repo = "" } = useParams()
  const repoQ = useRepo(owner, repo)
  const specsQ = useSpecsList(owner, repo)

  const specs = useMemo(() => specsQ.data?.specs ?? [], [specsQ.data])
  // Group once; `ordered` flattens the groups so the keyboard walk and the
  // default selection follow the same top-to-bottom order the tree renders.
  const groups = useMemo(() => groupByFolder(specs), [specs])
  const ordered = useMemo(() => groups.flatMap((g) => g.specs), [groups])

  const [searchParams, setSearchParams] = useSearchParams()
  const wanted = searchParams.get("path") ?? ""
  // The selected spec: the ?path= one if it's in the list, else the first.
  const selected = specs.find((s) => s.path === wanted) ?? ordered[0]
  const selectedPath = selected?.path ?? ""

  function selectSpec(path: string) {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        next.set("path", path)
        return next
      },
      { replace: true }
    )
  }
  useSpecKeyboardNav(ordered, selectedPath, selectSpec)

  if (repoQ.isLoading) return <Spinner />
  if (repoQ.error) return <ErrorMessage error={repoQ.error} />
  if (!repoQ.data) return null

  const r = repoQ.data

  return (
    <div className="specs-page">
      <OverviewCard owner={r.owner} repo={r.name} path="" summaries={{}} />

      {specsQ.isLoading && <Spinner label="Loading specs…" />}
      <ErrorMessage error={specsQ.error} />

      {specsQ.data && specs.length === 0 && <SpecsEmptyState />}

      {specs.length > 0 && (
        <div className="specs-layout">
          <aside className="specs-sidebar">
            <StatusRail specs={specs} />
            <SpecTree groups={groups} selectedPath={selectedPath} onSelect={selectSpec} />
          </aside>
          <main className="specs-main">
            <SpecView owner={r.owner} repo={r.name} path={selectedPath} />
          </main>
        </div>
      )}
    </div>
  )
}

// ── Status rail ──────────────────────────────────────────────────────────
// One-glance spec health: a count per lifecycle state. Only non-empty states
// show, so a repo of all-living specs reads as a single green chip.
function StatusRail({ specs }: { specs: SpecListItem[] }) {
  const counts = useMemo(() => {
    const c: Record<string, number> = {}
    for (const s of specs) {
      const k = statusKey(s.status)
      c[k] = (c[k] ?? 0) + 1
    }
    return c
  }, [specs])
  const order: Array<"living" | "draft" | "superseded"> = ["living", "draft", "superseded"]
  return (
    <div className="spec-rail">
      {order
        .filter((k) => counts[k])
        .map((k) => (
          <span key={k} className="spec-rail__item">
            <span className={`spec-dot spec-dot--${k}`} aria-hidden />
            {counts[k]} {k}
          </span>
        ))}
    </div>
  )
}

// ── Spec tree ──────────────────────────────────────────────────────────────
// Specs grouped by their folder under specs/. Root specs come first (no
// header), then each subfolder with its name as a header. Within a group,
// server order (by path) is preserved.
function SpecTree({
  groups,
  selectedPath,
  onSelect,
}: {
  groups: SpecGroup[]
  selectedPath: string
  onSelect: (path: string) => void
}) {
  return (
    <nav className="spec-tree" aria-label="Specs">
      {groups.map((g) => (
        <div key={g.folder} className="spec-tree__group">
          {g.folder && <div className="spec-tree__folder">{g.folder}</div>}
          <ul className="spec-tree__list">
            {g.specs.map((s) => {
              const active = s.path === selectedPath
              return (
                <li key={s.path}>
                  <button
                    type="button"
                    className={`spec-tree__item${active ? " is-active" : ""}`}
                    data-vim-selected={active || undefined}
                    aria-current={active || undefined}
                    onClick={() => onSelect(s.path)}
                  >
                    <span className={`spec-dot spec-dot--${statusKey(s.status)}`} aria-hidden />
                    <span className="spec-tree__title">{s.title}</span>
                  </button>
                </li>
              )
            })}
          </ul>
        </div>
      ))}
    </nav>
  )
}

// ── Spec view (center) ───────────────────────────────────────────────────
function SpecView({ owner, repo, path }: { owner: string; repo: string; path: string }) {
  const specQ = useSpec(owner, repo, path)
  if (specQ.isLoading) return <Spinner label="Loading spec…" />
  if (specQ.error) return <ErrorMessage error={specQ.error} />
  if (!specQ.data) return null
  const spec = specQ.data
  // The directory the spec lives in anchors its relative links/images.
  const basePath = path.split("/").slice(0, -1).join("/")
  return (
    <article className="spec-view">
      <SpecHeader spec={spec} />
      <Markdown content={spec.body} owner={owner} repo={repo} basePath={basePath} />
    </article>
  )
}

function SpecHeader({ spec }: { spec: SpecContent }) {
  const hasMeta = !!(spec.owners?.length || spec.covers?.length || spec.last_verified)
  return (
    <header className="spec-view__head">
      <div className="spec-view__title-row">
        <span className={`spec-dot spec-dot--${statusKey(spec.status)}`} aria-hidden />
        <h1 className="spec-view__title">{spec.title}</h1>
        {spec.status && <span className="spec-view__status muted small">{spec.status}</span>}
      </div>
      {hasMeta && (
        <div className="spec-view__meta muted small">
          {spec.owners && spec.owners.length > 0 && <span>owners: {spec.owners.join(", ")}</span>}
          {spec.covers && spec.covers.length > 0 && (
            <span>
              covers: {spec.covers.length} path{spec.covers.length === 1 ? "" : "s"}
            </span>
          )}
          {spec.last_verified && (
            <span>
              verified <RelativeTime iso={spec.last_verified} />
              {typeof spec.alignment === "number" &&
                ` · ${Math.round(spec.alignment * 100)}% aligned`}
            </span>
          )}
        </div>
      )}
    </header>
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

// ── helpers ────────────────────────────────────────────────────────────────

interface SpecGroup {
  folder: string // "" for specs at the specs/ root
  specs: SpecListItem[]
}

// groupByFolder buckets specs by their directory under specs/. The root group
// ("") sorts first; the rest alphabetically by folder.
function groupByFolder(specs: SpecListItem[]): SpecGroup[] {
  const byFolder = new Map<string, SpecListItem[]>()
  for (const s of specs) {
    const rel = s.path.replace(/^specs\//, "")
    const i = rel.lastIndexOf("/")
    const folder = i === -1 ? "" : rel.slice(0, i)
    const list = byFolder.get(folder) ?? []
    list.push(s)
    byFolder.set(folder, list)
  }
  return [...byFolder.keys()]
    .sort((a, b) => (a === "" ? -1 : b === "" ? 1 : a.localeCompare(b)))
    .map((folder) => ({ folder, specs: byFolder.get(folder) as SpecListItem[] }))
}

// statusKey normalizes a (possibly unknown) status into one of the status-dot
// modifier classes; anything unrecognized renders as the neutral "living" dot.
function statusKey(status?: string): "living" | "draft" | "superseded" {
  if (status === "draft" || status === "superseded") return status
  return "living"
}

// useSpecKeyboardNav lets ↑/↓ (and j/k) move the selection between specs in
// document order, mirroring the editable-target guard the shared keyboardNav
// util uses so it stays out of the way while typing. h/l remain the tab
// switcher's (useTabNav), so they're left untouched here.
function useSpecKeyboardNav(
  specs: SpecListItem[],
  selectedPath: string,
  onSelect: (path: string) => void
) {
  const specsRef = useRef(specs)
  specsRef.current = specs
  const selectedRef = useRef(selectedPath)
  selectedRef.current = selectedPath
  const onSelectRef = useRef(onSelect)
  onSelectRef.current = onSelect

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.metaKey || e.ctrlKey || e.altKey) return
      const el = document.activeElement as HTMLElement | null
      const tag = el?.tagName
      if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || el?.isContentEditable) return
      let delta = 0
      if (e.key === "ArrowDown" || e.key === "j") delta = 1
      else if (e.key === "ArrowUp" || e.key === "k") delta = -1
      else return
      const list = specsRef.current
      if (list.length === 0) return
      e.preventDefault()
      const cur = list.findIndex((s) => s.path === selectedRef.current)
      const next = Math.min(Math.max((cur === -1 ? 0 : cur) + delta, 0), list.length - 1)
      if (list[next] && list[next].path !== selectedRef.current) {
        onSelectRef.current(list[next].path)
      }
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [])
}
