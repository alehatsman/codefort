import "./specs.css"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { type ReactNode, useEffect, useMemo, useRef, useState } from "react"
import { Link, useParams, useSearchParams } from "react-router-dom"
import { api } from "@/api/client"
import { useCIRun, useRepo, useSpec, useSpecsList } from "@/api/queries"
import type { CIRun, SpecContent, SpecListItem, SpecVerification } from "@/api/types"
import CommandPalette, { type Command } from "@/features/specs/CommandPalette"
import QuickOpen from "@/features/specs/QuickOpen"
import SpecEditor from "@/features/specs/SpecEditor"
import Markdown from "@/shell/Markdown"
import OverviewCard from "@/shell/OverviewCard"
import { Button, EmptyState, ErrorMessage, RelativeTime, Spinner } from "@/ui"

/**
 * Specs tab: in-repo, human-authored specifications under specs/ — the dual of
 * the code itself ("what the code *is*"). A two-pane reader: a
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
  const wantedSection = searchParams.get("section") ?? ""
  // The selected spec: the ?path= one if it's in the list, else the first.
  const selected = specs.find((s) => s.path === wanted) ?? ordered[0]
  const selectedPath = selected?.path ?? ""

  // Update the selection in the URL. A section anchors the deep-link from spec
  // search; plain navigation (tree, ⌘P, ↑/↓) clears it so a stale anchor doesn't
  // linger.
  function setSelection(path: string, section?: string) {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        next.set("path", path)
        if (section) next.set("section", section)
        else next.delete("section")
        return next
      },
      { replace: true }
    )
  }
  function selectSpec(path: string) {
    setSelection(path)
  }
  useSpecKeyboardNav(ordered, selectedPath, selectSpec)

  const [paletteOpen, setPaletteOpen] = useState(false)
  const [cmdkOpen, setCmdkOpen] = useState(false)
  // A spec being created (not yet in the list): the editor opens on it directly.
  const [draft, setDraft] = useState<{ path: string; content: string } | null>(null)
  useSpecHotkeys({
    onJump: () => setPaletteOpen(true),
    onCommand: () => setCmdkOpen(true),
  })

  // The spec-workflow command registry. Later phases append their commands
  // (Verify, Draft, Plan→Issues, Score, Ask); the palette stays generic.
  const commands: Command[] = [
    {
      id: "new-spec",
      title: "New spec…",
      subtitle: "Create a spec under specs/",
      prompt: {
        placeholder: "spec name, e.g. ssh-transport",
        onSubmit: (name) => {
          const slug = name
            .trim()
            .replace(/\.md$/i, "")
            .replace(/[^a-zA-Z0-9/_-]+/g, "-")
            .replace(/^-+|-+$/g, "")
          if (slug) setDraft({ path: `specs/${slug}.md`, content: newSpecTemplate(slug) })
        },
      },
    },
    {
      id: "jump",
      title: "Jump to a spec…",
      subtitle: "Quick-open by name",
      shortcut: "⌘P",
      run: () => setPaletteOpen(true),
    },
  ]

  if (repoQ.isLoading) return <Spinner />
  if (repoQ.error) return <ErrorMessage error={repoQ.error} />
  if (!repoQ.data) return null

  const r = repoQ.data

  return (
    <div className="specs-page">
      <OverviewCard owner={r.owner} repo={r.name} path="" />

      {specsQ.isLoading && <Spinner label="Loading specs…" />}
      <ErrorMessage error={specsQ.error} />

      {draft ? (
        <main className="specs-main">
          <SpecEditor
            owner={r.owner}
            repo={r.name}
            path={draft.path}
            initialContent={draft.content}
            onDone={() => setDraft(null)}
          />
        </main>
      ) : specs.length > 0 ? (
        <div className="specs-layout">
          <aside className="specs-sidebar">
            <VerifyAllButton owner={r.owner} repo={r.name} />
            <StatusRail specs={specs} />
            <SpecTree groups={groups} selectedPath={selectedPath} onSelect={selectSpec} />
          </aside>
          <main className="specs-main">
            <SpecView owner={r.owner} repo={r.name} path={selectedPath} section={wantedSection} />
          </main>
        </div>
      ) : specsQ.data ? (
        <SpecsEmptyState onNew={() => setCmdkOpen(true)} />
      ) : null}

      <QuickOpen
        open={paletteOpen}
        specs={specs}
        onSelect={selectSpec}
        onClose={() => setPaletteOpen(false)}
      />
      <CommandPalette open={cmdkOpen} commands={commands} onClose={() => setCmdkOpen(false)} />
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
function SpecView({
  owner,
  repo,
  path,
  section,
}: {
  owner: string
  repo: string
  path: string
  section: string
}) {
  const specQ = useSpec(owner, repo, path)
  const articleRef = useRef<HTMLElement>(null)
  const [editing, setEditing] = useState(false)
  const data = specQ.data

  // Leaving a spec drops out of edit mode so the next one opens in read view.
  // biome-ignore lint/correctness/useExhaustiveDependencies: reset edit mode on spec change
  useEffect(() => {
    setEditing(false)
  }, [path])

  // Deep-link from spec search: once the spec has rendered, scroll the heading
  // matching ?section= into view (matched by its text, since the renderer's
  // headings carry no ids).
  useEffect(() => {
    if (!section || !data || editing) return
    const root = articleRef.current
    if (!root) return
    const headings = Array.from(root.querySelectorAll<HTMLElement>("h1, h2, h3, h4, h5, h6"))
    const match = headings.find((h) => h.textContent?.trim() === section)
    match?.scrollIntoView({ block: "start" })
  }, [section, data, editing])

  if (specQ.isLoading) return <Spinner label="Loading spec…" />
  if (specQ.error) return <ErrorMessage error={specQ.error} />
  if (!data) return null

  if (editing) {
    return (
      <SpecEditor
        owner={owner}
        repo={repo}
        path={path}
        initialContent={data.content}
        onDone={() => setEditing(false)}
      />
    )
  }

  // The directory the spec lives in anchors its relative links/images.
  const basePath = path.split("/").slice(0, -1).join("/")
  return (
    <article ref={articleRef} className="spec-view">
      <SpecHeader
        spec={data}
        actions={
          <div className="spec-view__actions">
            <VerifyButton owner={owner} repo={repo} path={path} />
            <Button variant="ghost" onClick={() => setEditing(true)}>
              Edit
            </Button>
            <Link className="spec-view__history" to={`/${owner}/${repo}/commits/${path}`}>
              History
            </Link>
          </div>
        }
      />
      {data.verification && <VerificationPanel v={data.verification} />}
      <Markdown content={data.body} owner={owner} repo={repo} basePath={basePath} />
    </article>
  )
}

// VerificationPanel is the truth surface for the read view: a collapsible "Last
// verification" card with the per-line markers (the gutter, listed by line),
// conflicts, and when/what it ran against. Open by default when the spec is
// stale or has drift, so a spec that needs attention shows it.
function VerificationPanel({ v }: { v: SpecVerification }) {
  const drift = v.markers.filter((m) => m.marker === "drifted")
  const attention = v.stale || drift.length > 0
  return (
    <details className="spec-verify" open={attention}>
      <summary className="spec-verify__summary">
        <span className="spec-verify__title">Last verification</span>
        <span className="muted small">
          {Math.round(v.alignment * 100)}% aligned · {drift.length} drifted ·{" "}
          <RelativeTime iso={v.verified_at} />
          {v.stale && <span className="spec-verify__stale"> · stale</span>}
        </span>
      </summary>
      {v.markers.length > 0 && (
        <ul className="spec-verify__markers">
          {v.markers.map((m) => (
            <li key={`${m.line}:${m.marker}`} className="spec-marker">
              <span
                className={`spec-marker__dot spec-marker--${markerKey(m.marker)}`}
                aria-hidden
              />
              <span className="spec-marker__line muted small">L{m.line}</span>
              <span className="spec-marker__text">{m.text || m.marker}</span>
              {m.note && <span className="spec-marker__note muted small">{m.note}</span>}
            </li>
          ))}
        </ul>
      )}
      {v.conflicts && v.conflicts.length > 0 && (
        <div className="spec-verify__conflicts">
          <span className="muted small">Conflicts</span>
          <ul>
            {v.conflicts.map((c) => (
              <li key={c}>{c}</li>
            ))}
          </ul>
        </div>
      )}
      {v.notes && <p className="spec-verify__notes muted small">{v.notes}</p>}
    </details>
  )
}

// markerKey normalizes a (possibly unknown) marker into a known modifier class.
function markerKey(marker: string): "aligned" | "drifted" | "unverifiable" | "unspecced" {
  if (marker === "drifted" || marker === "unverifiable" || marker === "unspecced") return marker
  return "aligned"
}

// terminalStatuses are the run states past which a verify run won't change — once
// reached, the watcher refreshes the spec so the gutter/rail reflect the result.
const terminalStatuses = new Set(["success", "failed", "canceled", "error", "interrupted"])

// VerifyButton kicks off a spec-verify run and, while it runs, shows a link to
// the SSE run viewer; on completion it refreshes the spec (gutter) and the list
// (status rail) so the new verdict appears in place.
function VerifyButton({ owner, repo, path }: { owner: string; repo: string; path: string }) {
  const [runNumber, setRunNumber] = useState<number | null>(null)
  const verify = useMutation<CIRun, Error, void>({
    mutationFn: () => api.verifySpec(owner, repo, path),
    onSuccess: (run) => setRunNumber(run.number),
  })
  return (
    <>
      <Button
        variant="ghost"
        disabled={verify.isPending || runNumber !== null}
        onClick={() => verify.mutate()}
      >
        {verify.isPending ? "Verifying…" : "Verify"}
      </Button>
      {runNumber !== null && (
        <VerifyWatch
          owner={owner}
          repo={repo}
          path={path}
          runNumber={runNumber}
          onDone={() => setRunNumber(null)}
        />
      )}
      <ErrorMessage error={verify.error} />
    </>
  )
}

function VerifyWatch({
  owner,
  repo,
  path,
  runNumber,
  onDone,
}: {
  owner: string
  repo: string
  path: string
  runNumber: number
  onDone: () => void
}) {
  const qc = useQueryClient()
  const runQ = useCIRun(owner, repo, runNumber)
  const status = runQ.data?.status
  // biome-ignore lint/correctness/useExhaustiveDependencies: refresh once the watched run reaches a terminal state
  useEffect(() => {
    if (status && terminalStatuses.has(status)) {
      void qc.invalidateQueries({ queryKey: ["spec", owner, repo, path] })
      void qc.invalidateQueries({ queryKey: ["specs", owner, repo] })
      onDone()
    }
  }, [status])
  return (
    <span className="spec-verify-status muted small">
      Verifying — <Link to={`/${owner}/${repo}/pipelines/${runNumber}`}>run #{runNumber}</Link>{" "}
      {status ?? "queued"}
    </span>
  )
}

// VerifyAllButton fans out: it reads the deterministic drift report and kicks
// off a verify run for every stale-candidate spec (stale or never-verified).
function VerifyAllButton({ owner, repo }: { owner: string; repo: string }) {
  const qc = useQueryClient()
  const verifyAll = useMutation<number, Error, void>({
    mutationFn: async () => {
      const drift = await api.specsDrift(owner, repo)
      const stale = drift.specs.filter((s) => s.status === "stale" || s.status === "unverified")
      await Promise.all(stale.map((s) => api.verifySpec(owner, repo, s.path)))
      return stale.length
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ["specs", owner, repo] })
    },
  })
  return (
    <div className="spec-verify-all">
      <Button variant="ghost" disabled={verifyAll.isPending} onClick={() => verifyAll.mutate()}>
        {verifyAll.isPending ? "Queuing…" : "Verify all stale"}
      </Button>
      {verifyAll.data !== undefined && (
        <span className="muted small">
          {verifyAll.data === 0 ? "Nothing stale" : `Queued ${verifyAll.data} run(s)`}
        </span>
      )}
      <ErrorMessage error={verifyAll.error} />
    </div>
  )
}

function SpecHeader({ spec, actions }: { spec: SpecContent; actions?: ReactNode }) {
  const hasMeta = !!(spec.owners?.length || spec.covers?.length || spec.last_verified)
  return (
    <header className="spec-view__head">
      <div className="spec-view__title-row">
        <span className={`spec-dot spec-dot--${statusKey(spec.status)}`} aria-hidden />
        <h1 className="spec-view__title">{spec.title}</h1>
        {spec.status && <span className="spec-view__status muted small">{spec.status}</span>}
        {spec.verification && (
          <span
            className={`spec-truth${spec.verification.stale ? " spec-truth--stale" : ""}`}
            title={
              spec.verification.stale ? "Code or spec changed since this was verified" : "Verified"
            }
          >
            {Math.round(spec.verification.alignment * 100)}% aligned
            {spec.verification.stale ? " · stale" : ""}
          </span>
        )}
        {actions}
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

function SpecsEmptyState({ onNew }: { onNew: () => void }) {
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
      <Button variant="primary" onClick={onNew}>
        New spec
      </Button>
    </EmptyState>
  )
}

// newSpecTemplate seeds a draft spec with the convention's frontmatter +
// sections so a fresh file starts well-formed (see docs/specs.md).
function newSpecTemplate(slug: string): string {
  const title = slug
    .split(/[-_/]/)
    .filter(Boolean)
    .map((w) => (w[0]?.toUpperCase() ?? "") + w.slice(1))
    .join(" ")
  return `---
id: ${slug}
status: draft
owners: []
covers: []
---
# ${title}

## Intent

## Behavior

## Checklist
- [ ]

## Non-goals
`
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
    const onKey = (e: KeyboardEvent) => handleSpecKeyboardNav(e, specsRef, selectedRef, onSelectRef)
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [])
}

// The keydown handler for useSpecKeyboardNav, pulled out to module scope so
// its branches don't stack cognitive complexity on top of the effect's.
function handleSpecKeyboardNav(
  e: KeyboardEvent,
  specsRef: { current: SpecListItem[] },
  selectedRef: { current: string },
  onSelectRef: { current: (path: string) => void }
): void {
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
  const target = list[next]
  if (target && target.path !== selectedRef.current) onSelectRef.current(target.path)
}

// useSpecHotkeys wires the Specs tab's palette shortcuts, taking over the
// browser defaults while the tab is mounted: ⌘K opens the command palette and
// ⌘P path quick-open (Sublime "go to").
function useSpecHotkeys({ onJump, onCommand }: { onJump: () => void; onCommand: () => void }) {
  const jumpRef = useRef(onJump)
  jumpRef.current = onJump
  const commandRef = useRef(onCommand)
  commandRef.current = onCommand
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => handleSpecHotkey(e, jumpRef, commandRef)
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [])
}

// The keydown handler for useSpecHotkeys, pulled out to module scope so its
// branches don't stack cognitive complexity on top of the effect's.
function handleSpecHotkey(
  e: KeyboardEvent,
  jumpRef: { current: () => void },
  commandRef: { current: () => void }
): void {
  const mod = e.metaKey || e.ctrlKey
  if (!mod || e.altKey) return
  const key = e.key.toLowerCase()
  if (key === "k" && !e.shiftKey) {
    e.preventDefault()
    commandRef.current()
  } else if (key === "p" && !e.shiftKey) {
    e.preventDefault()
    jumpRef.current()
  }
}
