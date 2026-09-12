import "./settings.css"
import { useState } from "react"
import { useNavigate, useParams } from "react-router-dom"
import {
  useAddRepoMember,
  useDeleteRepo,
  useRemoveRepoMember,
  useSetCIEnabled,
  useSetRepoVisibility,
} from "@/api/mutations"
import { useRepo, useRepoMembers } from "@/api/queries"
import type { AddMemberInput } from "@/api/types"
import OverviewCard from "@/shell/OverviewCard"
import { Button, ErrorMessage, SkeletonText, useToast } from "@/ui"

export default function RepoSettingsPage() {
  const { owner = "", repo = "" } = useParams()
  const repoQ = useRepo(owner, repo)

  if (repoQ.isLoading) return <SkeletonText lines={6} />
  if (repoQ.error) return <ErrorMessage error={repoQ.error} />
  if (!repoQ.data) return null

  const r = repoQ.data

  return (
    <div>
      <OverviewCard owner={owner} repo={repo} path="" />
      <div className="repo-settings">
        <GeneralSection
          owner={owner}
          repo={repo}
          ciEnabled={r.ci_enabled}
          visibility={r.visibility}
        />
        <MembersSection owner={owner} repo={repo} />
        <DangerSection owner={owner} repo={repo} />
      </div>
    </div>
  )
}

function GeneralSection({
  owner,
  repo,
  ciEnabled,
  visibility,
}: {
  owner: string
  repo: string
  ciEnabled: boolean
  visibility: "public" | "private"
}) {
  const setCI = useSetCIEnabled(owner, repo)
  const setVis = useSetRepoVisibility(owner, repo)
  const toast = useToast()

  function toggleCI() {
    setCI.mutate(!ciEnabled, {
      onSuccess: () => toast(`CI ${!ciEnabled ? "enabled" : "disabled"}`, { variant: "success" }),
    })
  }

  function toggleVisibility() {
    const next = visibility === "public" ? "private" : "public"
    setVis.mutate(next, {
      onSuccess: () => toast(`Repository is now ${next}`, { variant: "success" }),
    })
  }

  return (
    <section className="settings__section">
      <h2 className="settings__title">General</h2>
      <div className="repo-settings__rows">
        <div className="repo-settings__row">
          <div className="repo-settings__row-copy">
            <strong>Continuous integration</strong>
            <p className="muted small">
              Enable CI pipelines triggered on push. When off, the Pipelines tab shows a prompt to
              enable.
            </p>
          </div>
          <Button
            variant={ciEnabled ? "ghost" : "primary"}
            size="small"
            onClick={toggleCI}
            disabled={setCI.isPending}
          >
            {ciEnabled ? "Disable CI" : "Enable CI"}
          </Button>
        </div>

        <div className="repo-settings__row">
          <div className="repo-settings__row-copy">
            <strong>Visibility</strong>
            <p className="muted small">
              This repository is currently <strong>{visibility}</strong>. Changing to private
              restricts access to members only.
            </p>
          </div>
          <Button
            variant="ghost"
            size="small"
            onClick={toggleVisibility}
            disabled={setVis.isPending}
          >
            Make {visibility === "public" ? "private" : "public"}
          </Button>
        </div>
      </div>
      {setCI.isError && <ErrorMessage error={setCI.error} inline />}
      {setVis.isError && <ErrorMessage error={setVis.error} inline />}
    </section>
  )
}

function MembersSection({ owner, repo }: { owner: string; repo: string }) {
  const membersQ = useRepoMembers(owner, repo)
  const addMember = useAddRepoMember(owner, repo)
  const removeMember = useRemoveRepoMember(owner, repo)
  const toast = useToast()

  const [username, setUsername] = useState("")
  const [role, setRole] = useState<"read" | "write">("write")

  function submitAdd(e: { preventDefault(): void }) {
    e.preventDefault()
    const u = username.trim()
    if (!u) return
    const input: AddMemberInput = { username: u, role }
    addMember.mutate(input, {
      onSuccess: () => {
        setUsername("")
        toast(`Added ${u} as ${role}`, { variant: "success" })
      },
    })
  }

  return (
    <section className="settings__section">
      <h2 className="settings__title">Members</h2>
      <p className="muted settings__lead">Collaborators with access to this repository.</p>

      {membersQ.isLoading && <SkeletonText lines={2} />}
      <ErrorMessage error={membersQ.error} />

      {membersQ.data && membersQ.data.length > 0 && (
        <table className="token-table repo-settings__member-table">
          <thead>
            <tr>
              <th>Username</th>
              <th>Role</th>
              <th>Added</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {membersQ.data.map((m) => (
              <tr key={m.username}>
                <td>{m.username}</td>
                <td className="muted">{m.role}</td>
                <td className="muted small">{new Date(m.joined_at).toLocaleDateString()}</td>
                <td className="token-table__actions">
                  <Button
                    variant="ghost"
                    size="small"
                    disabled={removeMember.isPending}
                    onClick={() =>
                      removeMember.mutate(m.username, {
                        onSuccess: () => toast(`Removed ${m.username}`, { variant: "success" }),
                      })
                    }
                  >
                    Remove
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <form className="repo-settings__add-member" onSubmit={submitAdd}>
        <input
          className="input"
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          placeholder="username"
          spellCheck={false}
        />
        <select
          className="repo-settings__role-select"
          value={role}
          onChange={(e) => setRole(e.target.value as "read" | "write")}
          aria-label="Role"
        >
          <option value="write">write</option>
          <option value="read">read</option>
        </select>
        <Button variant="primary" size="small" type="submit" disabled={addMember.isPending}>
          Add member
        </Button>
        {addMember.isError && <ErrorMessage error={addMember.error} inline />}
      </form>
    </section>
  )
}

function DangerSection({ owner, repo }: { owner: string; repo: string }) {
  const del = useDeleteRepo()
  const navigate = useNavigate()
  const toast = useToast()
  const slug = `${owner}/${repo}`
  const [armed, setArmed] = useState(false)
  const [confirmText, setConfirmText] = useState("")
  const confirmed = confirmText.trim() === slug

  function onDelete() {
    if (!confirmed || del.isPending) return
    del.mutate(
      { owner, repo },
      {
        onSuccess: () => {
          toast(`Deleted ${slug}`, { variant: "success" })
          void navigate("/")
        },
      }
    )
  }

  return (
    <section className="settings__section">
      <h2 className="settings__title">Danger zone</h2>
      {!armed ? (
        <Button variant="danger" size="small" onClick={() => setArmed(true)}>
          Delete this repository
        </Button>
      ) : (
        <div className="danger-zone">
          <h3 className="danger-zone__title">Delete {slug}</h3>
          <div className="danger-zone__item">
            <div className="danger-zone__copy">
              <strong>This permanently deletes the repository.</strong>
              <p className="muted small">
                Removes <code>{slug}</code> and all its issues, pipeline runs, comments, pull reqs,
                and git data. This cannot be undone.
              </p>
            </div>
            <div className="danger-zone__action">
              <label className="danger-zone__confirm">
                <span className="muted small">
                  Type <code>{slug}</code> to confirm:
                </span>
                <input
                  className="input"
                  value={confirmText}
                  onChange={(e) => setConfirmText(e.target.value)}
                  placeholder={slug}
                  aria-label="Type the repository name to confirm deletion"
                />
              </label>
              <div className="repo-settings__danger-buttons">
                <Button variant="danger" disabled={!confirmed || del.isPending} onClick={onDelete}>
                  {del.isPending ? "Deleting…" : "Delete repository"}
                </Button>
                <Button variant="ghost" disabled={del.isPending} onClick={() => setArmed(false)}>
                  Cancel
                </Button>
              </div>
            </div>
            {del.isError && <ErrorMessage error={del.error} inline />}
          </div>
        </div>
      )}
    </section>
  )
}
