import { useEffect, useRef, useState } from "react"
import { useCreateIssue } from "@/api/mutations"
import { useRepos } from "@/api/queries"
import { Button, Dialog, ErrorMessage, FormField, Input, Select, Textarea } from "@/ui"

interface Props {
  // Repo-scoped use (the per-repo Issues/Board pages) pins the target via
  // owner+repo and the repo picker stays hidden. Omit both for the global
  // Issues view: a "Repository" dropdown appears and the target is chosen there.
  owner?: string
  repo?: string
  onCreated?: (issueNumber: number, owner: string, repo: string) => void
}

/**
 * Opens a modal dialog for creating an issue. Uses the native
 * <dialog> element via showModal() — gets us a real backdrop,
 * Escape-to-close, and platform focus trapping for free, with no
 * extra deps.
 *
 * State lives in this component; the mutation owns server state.
 * The dialog resets on close (both via submit success and explicit
 * cancel) so the next open starts fresh.
 *
 * When owner/repo aren't fixed by the caller, the target repo is picked from a
 * dropdown of all repos (the global Issues view); owner/repo always reach the
 * mutation in its variables either way.
 */
export default function NewIssueForm({ owner, repo, onCreated }: Props) {
  const dialogRef = useRef<HTMLDialogElement>(null)
  const titleRef = useRef<HTMLInputElement>(null)
  const [title, setTitle] = useState("")
  const [body, setBody] = useState("")
  // "owner/name" of the picked repo; only used when the caller didn't pin one.
  const [target, setTarget] = useState("")
  const mutation = useCreateIssue()

  const needsPicker = !owner || !repo
  // Only fetch the repo list when the picker is actually shown.
  const { data: repos } = useRepos(needsPicker)

  // Clear the form whenever the dialog closes (submit success, Esc, or backdrop).
  // Autofocus is handled in open() once the dialog is rendered.
  useEffect(() => {
    const dialog = dialogRef.current
    if (!dialog) return
    const reset = () => {
      setTitle("")
      setBody("")
      mutation.reset()
    }
    dialog.addEventListener("close", reset)
    return () => dialog.removeEventListener("close", reset)
  }, [mutation.reset])

  function open() {
    // Default the picker to the first repo so a submit is one field away.
    if (needsPicker && !target && repos && repos.length > 0) {
      setTarget(`${repos[0].owner}/${repos[0].name}`)
    }
    dialogRef.current?.showModal()
    // Microtask so the dialog is rendered before we try to focus.
    queueMicrotask(() => titleRef.current?.focus())
  }

  function close() {
    dialogRef.current?.close()
  }

  // The chosen target: caller-pinned owner/repo, else the picked "owner/name".
  function resolveTarget(): { owner: string; repo: string } | null {
    if (owner && repo) return { owner, repo }
    const slash = target.indexOf("/")
    if (slash < 0) return null
    return { owner: target.slice(0, slash), repo: target.slice(slash + 1) }
  }

  function submit(e: React.FormEvent) {
    e.preventDefault()
    const trimmed = title.trim()
    const dest = resolveTarget()
    if (!trimmed || !dest || mutation.isPending) return
    mutation.mutate(
      { owner: dest.owner, repo: dest.repo, title: trimmed, body: body.trim() || undefined },
      {
        onSuccess: (created) => {
          close()
          onCreated?.(created.number, dest.owner, dest.repo)
        },
      }
    )
  }

  const canSubmit = title.trim() !== "" && resolveTarget() !== null && !mutation.isPending

  return (
    <>
      <Button variant="primary" onClick={open}>
        + New issue
      </Button>

      <Dialog
        ref={dialogRef}
        title="New issue"
        onClose={close}
        onSubmit={submit}
        footer={
          <>
            <Button onClick={close} disabled={mutation.isPending}>
              Cancel
            </Button>
            <Button type="submit" variant="primary" disabled={!canSubmit}>
              {mutation.isPending ? "Creating…" : "Submit new issue"}
            </Button>
          </>
        }
      >
        {needsPicker && (
          <FormField label="Repository">
            {({ controlId, describedBy }) => (
              <Select
                id={controlId}
                aria-describedby={describedBy}
                value={target}
                onChange={(e) => setTarget(e.target.value)}
                required
              >
                {(!repos || repos.length === 0) && <option value="">No repositories</option>}
                {repos?.map((r) => (
                  <option key={`${r.owner}/${r.name}`} value={`${r.owner}/${r.name}`}>
                    {r.owner}/{r.name}
                  </option>
                ))}
              </Select>
            )}
          </FormField>
        )}
        <FormField label="Title">
          {({ controlId, describedBy }) => (
            <Input
              id={controlId}
              aria-describedby={describedBy}
              ref={titleRef}
              placeholder="Short summary"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              required
            />
          )}
        </FormField>
        <FormField label="Description">
          {({ controlId, describedBy }) => (
            <Textarea
              id={controlId}
              aria-describedby={describedBy}
              placeholder="Optional — what's the problem or task?"
              value={body}
              onChange={(e) => setBody(e.target.value)}
              rows={8}
            />
          )}
        </FormField>
        <ErrorMessage error={mutation.error} />
      </Dialog>
    </>
  )
}
