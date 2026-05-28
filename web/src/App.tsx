import { useState } from "react";
import { Route, Routes } from "react-router-dom";
import Layout from "./components/Layout";
import TokenGate from "./components/TokenGate";
import { getToken } from "./api/client";
import ReposPage from "./routes/ReposPage";
import RepoPage from "./routes/RepoPage";
import IssuesPage from "./routes/IssuesPage";
import IssuePage from "./routes/IssuePage";

export default function App() {
  const [hasToken, setHasToken] = useState<boolean>(() => getToken() !== null);

  if (!hasToken) {
    return <TokenGate onSet={() => setHasToken(true)} />;
  }

  return (
    <Layout onSignOut={() => setHasToken(false)}>
      <Routes>
        <Route path="/" element={<ReposPage />} />
        <Route path="/:owner/:repo" element={<RepoPage />} />
        <Route path="/:owner/:repo/issues" element={<IssuesPage />} />
        <Route path="/:owner/:repo/issues/:number" element={<IssuePage />} />
      </Routes>
    </Layout>
  );
}
