import { useState } from "react"
import { useNavigate, useParams } from "react-router-dom"
import { ApiError } from "../api/client"
import { useRepo } from "../api/queries"
import { useDeleteRepo } from "../api/mutations"
import NotFound from "../components/NotFound"
import { Button, ErrorMessage, Input, Spinner } from "../components/ui"

// Per-repo settings. Today it hosts a single Danger Zone — deleting the repo —
// but it's the natural home for future per-repo settings (the CI opt-in could
// move here). Mirrors the global SettingsPage's section layout.
export default function RepoSettingsPage() {
  const { owner = "", repo = "" } = useParams()
  const repoQ = useRepo(owner, repo)

  if (repoQ.isLoading) return <Spinner />
  if (repoQ.error) {
    const err = repoQ.error
    if (err instanceof ApiError && err.status === 404) {
      return (
        <NotFound title="Repository not found" detail={`${owner}/${repo} isn’t registered here.`} />
      )
    }
    return <ErrorMessage error={err} />
  }
  if (!repoQ.data) return null

  return (
    <div className="repo">
      <section className="settings__section">
        <h2 className="settings__title">Settings</h2>
        <DangerZone owner={repoQ.data.owner} repo={repoQ.data.name} />
      </section>
    </div>
  )
}

// DangerZone gates the irreversible delete behind a type-to-confirm: the user
// must type the repo's exact owner/name slug before the Delete button enables.
// Stricter than the issue-delete window.confirm because this removes a whole
// repo and all its data. On success it navigates to the repos list.
function DangerZone({ owner, repo }: { owner: string; repo: string }) {
  const navigate = useNavigate()
  const del = useDeleteRepo()
  const [confirmText, setConfirmText] = useState("")

  const slug = `${owner}/${repo}`
  const armed = confirmText.trim() === slug

  function onDelete() {
    if (!armed || del.isPending) return
    del.mutate({ owner, repo }, { onSuccess: () => navigate("/") })
  }

  return (
    <div className="danger-zone">
      <h3 className="danger-zone__title">Danger Zone</h3>
      <div className="danger-zone__item">
        <div className="danger-zone__copy">
          <strong>Delete this repository</strong>
          <p className="muted small">
            Removes the repository and all its issues, pipeline runs, comments, pull requests, and
            git data. This cannot be undone.
          </p>
        </div>
        <div className="danger-zone__action">
          <label className="danger-zone__confirm">
            <span className="muted small">
              Type <code>{slug}</code> to confirm:
            </span>
            <Input
              value={confirmText}
              onChange={(e) => setConfirmText(e.target.value)}
              placeholder={slug}
              aria-label="Type the repository name to confirm deletion"
              autoComplete="off"
            />
          </label>
          <Button variant="danger" disabled={!armed || del.isPending} onClick={onDelete}>
            {del.isPending ? "Deleting…" : "Delete repository"}
          </Button>
        </div>
      </div>
      {del.error && <ErrorMessage error={del.error} inline />}
    </div>
  )
}
