import { useState } from "react"
import { Route, Routes } from "react-router-dom"
import { getToken } from "@/api/client"
import AgentsPage from "@/features/agents/AgentsPage"
import GlobalAgentsPage from "@/features/agents/GlobalAgentsPage"
import CommitPage from "@/features/commits/CommitPage"
import CommitsPage from "@/features/commits/CommitsPage"
import BoardPage from "@/features/issues/BoardPage"
import GlobalBoardPage from "@/features/issues/GlobalBoardPage"
import GlobalIssuesPage from "@/features/issues/GlobalIssuesPage"
import IssuePage from "@/features/issues/IssuePage"
import IssuesPage from "@/features/issues/IssuesPage"
import GlobalRunsPage from "@/features/pipelines/GlobalRunsPage"
import PipelinesPage from "@/features/pipelines/PipelinesPage"
import ComparePage from "@/features/pulls/ComparePage"
import GlobalPullsPage from "@/features/pulls/GlobalPullsPage"
import PullPage from "@/features/pulls/PullPage"
import PullsPage from "@/features/pulls/PullsPage"
import ReviewPage from "@/features/pulls/ReviewPage"
import BranchesPage from "@/features/repo/BranchesPage"
import IndexPage from "@/features/repo/IndexPage"
import RepoPage from "@/features/repo/RepoPage"
import ReposPage from "@/features/repo/ReposPage"
import TagsPage from "@/features/repo/TagsPage"
import RepoSettingsPage from "@/features/settings/RepoSettingsPage"
import SettingsPage from "@/features/settings/SettingsPage"
import TokenGate from "@/features/settings/TokenGate"
import SpecsPage from "@/features/specs/SpecsPage"
import Layout from "@/shell/Layout"
import NotFound from "@/shell/NotFound"
import DevGalleryPage from "@/ui/DevGalleryPage"

export default function App() {
  const [hasToken, setHasToken] = useState<boolean>(() => getToken() !== null)

  if (!hasToken) {
    return <TokenGate onSet={() => setHasToken(true)} />
  }

  return (
    <Layout onSignOut={() => setHasToken(false)}>
      <Routes>
        <Route path="/" element={<IndexPage />} />
        <Route path="/repos" element={<ReposPage />} />
        {/* Top-level cross-repo aggregate views (the global nav tabs). Static
            paths, so they rank above the /:owner/:repo dynamic route. */}
        <Route path="/issues" element={<GlobalIssuesPage />} />
        <Route path="/issues/board" element={<GlobalBoardPage />} />
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
        <Route path="/:owner/:repo/branches" element={<BranchesPage />} />
        <Route path="/:owner/:repo/tags" element={<TagsPage />} />
        <Route path="/:owner/:repo/specs" element={<SpecsPage />} />
        <Route path="/:owner/:repo/review" element={<ReviewPage />} />
        <Route path="/:owner/:repo/pipelines" element={<PipelinesPage />} />
        <Route path="/:owner/:repo/pipelines/:number" element={<PipelinesPage />} />
        <Route path="/:owner/:repo/agents" element={<AgentsPage />} />
        <Route path="/:owner/:repo/agents/:number" element={<PipelinesPage kind="agent" />} />
        <Route path="/:owner/:repo/settings" element={<RepoSettingsPage />} />
        <Route path="/settings" element={<SettingsPage />} />
        {/* Living gallery of the base UI primitives (src/ui). Dev tool. */}
        <Route path="/dev/ui" element={<DevGalleryPage />} />
        <Route path="*" element={<NotFound detail="This page doesn’t exist." />} />
      </Routes>
    </Layout>
  )
}
