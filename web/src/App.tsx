import { useState } from "react"
import { Navigate, Route, Routes, useParams } from "react-router-dom"
import Layout from "./components/Layout"
import TokenGate from "./components/TokenGate"
import NotFound from "./components/NotFound"
import { getToken } from "./api/client"
import ReposPage from "./routes/ReposPage"
import RepoPage from "./routes/RepoPage"
import CommitsPage from "./routes/CommitsPage"
import CommitPage from "./routes/CommitPage"
import IssuesPage from "./routes/IssuesPage"
import IssuePage from "./routes/IssuePage"
import ComparePage from "./routes/ComparePage"
import PullsPage from "./routes/PullsPage"
import PullPage from "./routes/PullPage"
import BoardPage from "./routes/BoardPage"
import ExplorePage from "./routes/ExplorePage"
import ReviewPage from "./routes/ReviewPage"
import PipelinesPage from "./routes/PipelinesPage"
import SettingsPage from "./routes/SettingsPage"

// The old Research and Summaries tabs merged into one Explore tab; keep their
// URLs working by redirecting to the merged page.
function ExploreRedirect() {
  const { owner = "", repo = "" } = useParams()
  return <Navigate to={`/${owner}/${repo}/explore`} replace />
}

export default function App() {
  const [hasToken, setHasToken] = useState<boolean>(() => getToken() !== null)

  if (!hasToken) {
    return <TokenGate onSet={() => setHasToken(true)} />
  }

  return (
    <Layout onSignOut={() => setHasToken(false)}>
      <Routes>
        <Route path="/" element={<ReposPage />} />
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
        <Route path="/:owner/:repo/agents" element={<PipelinesPage kind="agent" />} />
        <Route path="/:owner/:repo/agents/:number" element={<PipelinesPage kind="agent" />} />
        <Route path="/settings" element={<SettingsPage />} />
        <Route path="*" element={<NotFound detail="This page doesn’t exist." />} />
      </Routes>
    </Layout>
  )
}
