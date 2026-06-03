import { useState } from "react"
import { Navigate, Route, Routes, useParams } from "react-router-dom"
import Layout from "@/shell/Layout"
import TokenGate from "@/features/settings/TokenGate"
import NotFound from "@/shell/NotFound"
import { getToken } from "@/api/client"
import ReposPage from "@/features/repo/ReposPage"
import GlobalIssuesPage from "@/features/issues/GlobalIssuesPage"
import GlobalPullsPage from "@/features/pulls/GlobalPullsPage"
import GlobalRunsPage from "@/features/pipelines/GlobalRunsPage"
import GlobalAgentsPage from "@/features/agents/GlobalAgentsPage"
import AgentsPage from "@/features/agents/AgentsPage"
import RepoPage from "@/features/repo/RepoPage"
import CommitsPage from "@/features/commits/CommitsPage"
import CommitPage from "@/features/commits/CommitPage"
import IssuesPage from "@/features/issues/IssuesPage"
import IssuePage from "@/features/issues/IssuePage"
import ComparePage from "@/features/pulls/ComparePage"
import PullsPage from "@/features/pulls/PullsPage"
import PullPage from "@/features/pulls/PullPage"
import BoardPage from "@/features/issues/BoardPage"
import ExplorePage from "@/features/explore/ExplorePage"
import ReviewPage from "@/features/pulls/ReviewPage"
import PipelinesPage from "@/features/pipelines/PipelinesPage"
import RepoSettingsPage from "@/features/settings/RepoSettingsPage"
import SettingsPage from "@/features/settings/SettingsPage"
import DevGalleryPage from "@/ui/DevGalleryPage"

// The old Research and Summaries tabs merged into one Explore tab; keep their
// URLs working by redirecting to the merged page.
const ExploreRedirect = () => {
  const { owner = "", repo = "" } = useParams()
  return <Navigate to={`/${owner}/${repo}/explore`} replace />
}

const App = () => {
  const [hasToken, setHasToken] = useState<boolean>(() => getToken() !== null)

  if (!hasToken) {
    return <TokenGate onSet={() => setHasToken(true)} />
  }

  return (
    <Layout onSignOut={() => setHasToken(false)}>
      <Routes>
        <Route path="/" element={<ReposPage />} />
        {/* Top-level cross-repo aggregate views (the global nav tabs). Static
            paths, so they rank above the /:owner/:repo dynamic route. */}
        <Route path="/issues" element={<GlobalIssuesPage />} />
        <Route path="/pulls" element={<GlobalPullsPage />} />
        <Route path="/pipelines" element={<GlobalRunsPage kind="ci" />} />
        <Route path="/agents" element={<GlobalAgentsPage />} />
        <Route path="/:owner/:repo" element={<RepoPage />} />
        <Route path="/:owner/:repo/tree/*" element={<RepoPage />} />
        <Route path="/:owner/:repo/blob/*" element={<RepoPage />} />
        <Route path="/:owner/:repo/commits/*" element={<CommitsPage />} />
        <Route path="/:owner/:repo/commit/:sha" element={<CommitPage />} />
        <Route path="/:owner/:repo/issues" element={<IssuesPage />} />
        <Route path="/:owner/:repo/issues/board" element={<BoardPage />} />
        <Route path="/:owner/:repo/issues/:number" element={<IssuePage />} />
        <Route path="/:owner/:repo/compare" element={<ComparePage />} />
        <Route path="/:owner/:repo/pulls" element={<PullsPage />} />
        <Route path="/:owner/:repo/pulls/:number" element={<PullPage />} />
        <Route path="/:owner/:repo/explore" element={<ExplorePage />} />
        <Route path="/:owner/:repo/research" element={<ExploreRedirect />} />
        <Route path="/:owner/:repo/summaries" element={<ExploreRedirect />} />
        <Route path="/:owner/:repo/review" element={<ReviewPage />} />
        <Route path="/:owner/:repo/pipelines" element={<PipelinesPage />} />
        <Route path="/:owner/:repo/pipelines/:number" element={<PipelinesPage />} />
        <Route path="/:owner/:repo/agents" element={<AgentsPage />} />
        <Route path="/:owner/:repo/agents/:number" element={<PipelinesPage kind="agent" />} />
        <Route path="/:owner/:repo/settings" element={<RepoSettingsPage />} />
        <Route path="/settings" element={<SettingsPage />} />
        {/* Living gallery of the base UI primitives (components/ui). Dev tool. */}
        <Route path="/dev/ui" element={<DevGalleryPage />} />
        <Route path="*" element={<NotFound detail="This page doesn’t exist." />} />
      </Routes>
    </Layout>
  )
}

export default App
