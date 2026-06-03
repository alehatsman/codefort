import { useRef, useState } from "react"
import { useNavigate } from "react-router-dom"
import { useCreatePull } from "@/api/mutations"
import { useRefs, useRepos } from "@/api/queries"
import { Button, Dialog, ErrorMessage, Field, Input, Select, Spinner, Textarea } from "@/ui"

/**
 * Fleet-wide "open a pull request" modal for the global /pulls page. Unlike the
 * per-repo ComparePage (which already knows its repo from the route), this picks
 * the project first, then loads that repo's branches to choose base + head.
 * Creation goes straight through useCreatePull; on success we navigate to the
 * new PR. State is local and resets on close, mirroring NewRepoForm.
 */
export default function NewGlobalPullForm() {
  const dialogRef = useRef<HTMLDialogElement>(null)
  const navigate = useNavigate()

  // "owner/name" of the chosen repo — empty until one is picked. Splitting it
  // back out keeps a single <select> value while feeding the per-repo hooks.
  const [repoKey, setRepoKey] = useState("")
  const [owner, name] = repoKey ? repoKey.split("/") : ["", ""]

  const [base, setBase] = useState("")
  const [head, setHead] = useState("")
  const [title, setTitle] = useState("")
  const [body, setBody] = useState("")

  const reposQ = useRepos()
  const repos = reposQ.data ?? []
  // Branches load once a project is selected; useRefs is gated on owner+repo.
  const refsQ = useRefs(owner, name)
  const branches = refsQ.data?.branches ?? []
  const def = refsQ.data?.default ?? ""

  const createPull = useCreatePull(owner, name)

  // base defaults to the repo's default branch the first time branches arrive;
  // the user can still override either picker.
  const effectiveBase = base || def

  function reset() {
    setRepoKey("")
    setBase("")
    setHead("")
    setTitle("")
    setBody("")
    createPull.reset()
  }

  function open() {
    dialogRef.current?.showModal()
  }

  function close() {
    dialogRef.current?.close()
    reset()
  }

  function onPickRepo(value: string) {
    // Switching projects invalidates the branch selection from the old repo.
    setRepoKey(value)
    setBase("")
    setHead("")
    createPull.reset()
  }

  const sameBranch = effectiveBase === head && head !== ""
  const trimmedTitle = title.trim()
  const canCreate =
    !!repoKey && !!effectiveBase && !!head && !sameBranch && !!trimmedTitle && !createPull.isPending

  function submit(e: React.FormEvent) {
    e.preventDefault()
    if (!canCreate) return
    createPull.mutate(
      { base: effectiveBase, head, title: trimmedTitle, body: body.trim() || undefined },
      {
        onSuccess: (pr) => {
          close()
          navigate(`/${owner}/${name}/pulls/${pr.number}`)
        },
      }
    )
  }

  return (
    <>
      <Button variant="primary" onClick={open}>
        + New pr
      </Button>

      <Dialog
        ref={dialogRef}
        title="Open a pull request"
        onClose={close}
        onSubmit={submit}
        footer={
          <>
            <Button onClick={close} disabled={createPull.isPending}>
              Cancel
            </Button>
            <Button type="submit" variant="primary" disabled={!canCreate}>
              {createPull.isPending ? "Creating…" : "Create pull request"}
            </Button>
          </>
        }
      >
        <Field label="Project">
          <Select value={repoKey} onChange={(e) => onPickRepo(e.target.value)} required>
            <option value="">choose a project…</option>
            {repos.map((r) => (
              <option key={`${r.owner}/${r.name}`} value={`${r.owner}/${r.name}`}>
                {r.owner}/{r.name}
              </option>
            ))}
          </Select>
        </Field>

        {repoKey && refsQ.isLoading && <Spinner />}

        {repoKey && !refsQ.isLoading && (
          <>
            <Field label="Base branch">
              <Select
                value={effectiveBase}
                onChange={(e) => setBase(e.target.value)}
                aria-label="Base branch"
              >
                {branches.map((b) => (
                  <option key={b} value={b}>
                    {b}
                  </option>
                ))}
              </Select>
            </Field>
            <Field label="Head branch">
              <Select
                value={head}
                onChange={(e) => setHead(e.target.value)}
                aria-label="Head branch"
              >
                <option value="">choose a branch…</option>
                {branches.map((b) => (
                  <option key={b} value={b}>
                    {b}
                  </option>
                ))}
              </Select>
            </Field>
          </>
        )}

        <Field label="Title">
          <Input
            placeholder="Pull request title"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            required
          />
        </Field>
        <Field label="Description">
          <Textarea
            placeholder="Description (optional)"
            value={body}
            onChange={(e) => setBody(e.target.value)}
          />
        </Field>

        {sameBranch && (
          <div className="field__label muted">Pick two different branches to compare.</div>
        )}
        <ErrorMessage error={refsQ.error} />
        <ErrorMessage error={createPull.error} />
      </Dialog>
    </>
  )
}
