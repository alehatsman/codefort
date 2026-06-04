import { useMemo, useRef } from "react"
import "./repo.css"
import { Link, useNavigate } from "react-router-dom"
import {
  DndContext,
  DragOverlay,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core"
import { SortableContext, arrayMove, rectSortingStrategy, useSortable } from "@dnd-kit/sortable"
import { CSS } from "@dnd-kit/utilities"
import { useRepos } from "@/api/queries"
import CIStatusIcon from "@/features/pipelines/CIStatusIcon"
import ActivityFeed from "@/features/repo/ActivityFeed"
import FleetCounters from "@/features/repo/FleetCounters"
import NewRepoForm from "@/features/repo/NewRepoForm"
import { applyOrder, useRepoOrder } from "@/features/repo/useRepoOrder"
import { useFleetEvents } from "@/features/repo/useFleetEvents"
import { Button, Card, EmptyState, ErrorMessage, PageHeader, SkeletonText } from "@/ui"
import type { Repo } from "@/api/types"
import { useListNav } from "@/shell/keyboardNav"

export default function ReposPage() {
  const { data, isLoading, error } = useRepos()
  const events = useFleetEvents()
  const navigate = useNavigate()
  const { order, reorder, resetOrder, isCustom } = useRepoOrder()

  const sorted = useMemo(() => applyOrder(data ?? [], order), [data, order])
  const ids = sorted.map((r) => `${r.owner}/${r.name}`)

  const gridRef = useRef<HTMLDivElement>(null)
  const { index } = useListNav({
    count: sorted.length,
    getColumns: () => gridColumnCount(gridRef.current),
    onActivate: (i) => {
      const r = sorted[i]
      if (r) navigate(`/${r.owner}/${r.name}`)
    },
  })

  // 8px activation distance so a quick click on a link inside the card still
  // navigates instead of starting a drag.
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 8 } }))

  function onDragEnd({ active, over }: DragEndEvent) {
    if (!over || active.id === over.id) return
    const oldIndex = ids.indexOf(active.id as string)
    const newIndex = ids.indexOf(over.id as string)
    if (oldIndex === -1 || newIndex === -1) return
    const next = arrayMove(ids, oldIndex, newIndex)
    reorder(next)
  }

  return (
    <div className="repos">
      <PageHeader
        title="Repositories"
        actions={
          <>
            {isCustom && (
              <Button size="small" onClick={resetOrder} title="Restore creation order">
                Reset order
              </Button>
            )}
            <NewRepoForm onCreated={(owner, name) => navigate(`/${owner}/${name}`)} />
          </>
        }
      />

      <FleetCounters />

      <h3 className="repos__feed-title muted small">Recent activity</h3>
      <ActivityFeed events={events} />

      {isLoading && (
        <div className="card-grid" aria-hidden="true">
          {Array.from({ length: 6 }, (_, i) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: fixed-count static placeholder, never reorders
            <Card key={i}>
              <SkeletonText lines={2} />
            </Card>
          ))}
        </div>
      )}
      {error && <ErrorMessage error={error} />}

      {!isLoading && !error && (!data || data.length === 0) && (
        <EmptyState>No repos registered yet. Create one with the button above.</EmptyState>
      )}

      {data && data.length > 0 && (
        <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={onDragEnd}>
          <SortableContext items={ids} strategy={rectSortingStrategy}>
            <div className="card-grid" ref={gridRef}>
              {sorted.map((r, i) => (
                <SortableRepoCard key={`${r.owner}/${r.name}`} repo={r} selected={i === index} />
              ))}
            </div>
          </SortableContext>
          <DragOverlay>
            {/* DragOverlay renders the floating ghost; content is handled by the
                sortable item's transform so we don't need a separate ghost card */}
            {null}
          </DragOverlay>
        </DndContext>
      )}
    </div>
  )
}

function SortableRepoCard({ repo, selected }: { repo: Repo; selected: boolean }) {
  const id = `${repo.owner}/${repo.name}`
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id,
  })

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.4 : undefined,
    cursor: isDragging ? "grabbing" : undefined,
  }

  return (
    <Card
      ref={setNodeRef}
      style={style}
      selected={selected}
      data-vim-selected={selected ? "true" : undefined}
      {...attributes}
      {...listeners}
    >
      <div className="card__title">
        <Link
          to={`/${repo.owner}/${repo.name}`}
          // Prevent the link from hijacking the drag gesture. The PointerSensor's
          // activation distance means a plain click still follows the link.
          draggable={false}
        >
          <span className="muted">{repo.owner}/</span>
          {repo.name}
        </Link>
        <span className="card__created muted">
          {new Date(repo.created_at).toLocaleDateString()}
        </span>
      </div>
      <RepoMetrics repo={repo} />
    </Card>
  )
}

function RepoMetrics({ repo: r }: { repo: Repo }) {
  const base = `/${r.owner}/${r.name}`
  const ciTo = r.ci_status ? `${base}/pipelines/${r.ci_number}` : `${base}/pipelines`
  return (
    <div className="repo-metrics">
      <Link className="repo-metric" to={ciTo} title={r.ci_status ? `CI ${r.ci_status}` : "CI"}>
        <span className="repo-metric__value">
          {r.ci_status ? <CIStatusIcon status={r.ci_status} /> : <span className="muted">—</span>}
        </span>
        <span className="repo-metric__label">CI</span>
      </Link>
      <MetricTile to={`${base}/issues`} value={r.open_issues} label="Issues" />
      <MetricTile to={`${base}/pulls`} value={r.open_pulls} label="PRs" />
      <MetricTile to={`${base}/review`} value={r.open_reviews} label="Reviews" />
      <MetricTile to={`${base}/agents`} value={r.active_agents} label="Agents" />
    </div>
  )
}

function MetricTile({ to, value, label }: { to: string; value: number; label: string }) {
  return (
    <Link className="repo-metric" to={to} title={`${value} ${label.toLowerCase()}`}>
      <span className="repo-metric__value" data-zero={value === 0 ? "true" : undefined}>
        {value}
      </span>
      <span className="repo-metric__label">{label}</span>
    </Link>
  )
}

function gridColumnCount(grid: HTMLDivElement | null): number {
  if (!grid) return 1
  const tracks = getComputedStyle(grid).gridTemplateColumns.split(" ").filter(Boolean)
  return tracks.length || 1
}
