import { useState } from "react"
import { Route, Routes } from "react-router-dom"
import Layout from "./components/Layout"
import TokenGate from "./components/TokenGate"
import { getToken } from "./api/client"
import ReposPage from "./routes/ReposPage"
import RepoPage from "./routes/RepoPage"
import CommitsPage from "./routes/CommitsPage"
import CommitPage from "./routes/CommitPage"
import IssuesPage from "./routes/IssuesPage"
import IssuePage from "./routes/IssuePage"
import BoardPage from "./routes/BoardPage"
import ResearchPage from "./routes/ResearchPage"
import PipelinesPage from "./routes/PipelinesPage"
import SettingsPage from "./routes/SettingsPage"

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
        <Route path="/:owner/:repo/research" element={<ResearchPage />} />
        <Route path="/:owner/:repo/pipelines" element={<PipelinesPage />} />
        <Route path="/:owner/:repo/pipelines/:number" element={<PipelinesPage />} />
        <Route path="/settings" element={<SettingsPage />} />
      </Routes>
    </Layout>
  )
}
