import "./repo.css"
import ActivityFeed from "@/features/repo/ActivityFeed"
import FleetCounters from "@/features/repo/FleetCounters"
import { useFleetEvents } from "@/features/repo/useFleetEvents"
import { PageHeader } from "@/ui"

export default function IndexPage() {
  const events = useFleetEvents()

  return (
    <div className="repos">
      <PageHeader title="Overview" />
      <FleetCounters />
      <h3 className="repos__feed-title muted small">Recent activity</h3>
      <ActivityFeed events={events} />
    </div>
  )
}
