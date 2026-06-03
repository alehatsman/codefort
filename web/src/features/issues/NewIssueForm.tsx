import { useEffect, useRef, useState } from "react"
import { useCreateIssue } from "@/api/mutations"
import { Button, Dialog, ErrorMessage, Field, Input, Textarea } from "@/ui"

interface Props {
  owner: string
  repo: string
  onCreated?: (issueNumber: number) => void
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
 */
export default function NewIssueForm({ owner, repo, onCreated }: Props) {
  const dialogRef = useRef<HTMLDialogElement>(null)
  const titleRef = useRef<HTMLInputElement>(null)
  const [title, setTitle] = useState("")
  const [body, setBody] = useState("")
  const mutation = useCreateIssue(owner, repo)

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
    dialogRef.current?.showModal()
    // Microtask so the dialog is rendered before we try to focus.
    queueMicrotask(() => titleRef.current?.focus())
  }

  function close() {
    dialogRef.current?.close()
  }

  function submit(e: React.FormEvent) {
    e.preventDefault()
    const trimmed = title.trim()
    if (!trimmed || mutation.isPending) return
    mutation.mutate(
      { title: trimmed, body: body.trim() || undefined },
      {
        onSuccess: (created) => {
          close()
          onCreated?.(created.number)
        },
      }
    )
  }

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
            <Button type="submit" variant="primary" disabled={!title.trim() || mutation.isPending}>
              {mutation.isPending ? "Creating…" : "Submit new issue"}
            </Button>
          </>
        }
      >
        <Field label="Title">
          <Input
            ref={titleRef}
            placeholder="Short summary"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            required
          />
        </Field>
        <Field label="Description">
          <Textarea
            placeholder="Optional — what's the problem or task?"
            value={body}
            onChange={(e) => setBody(e.target.value)}
            rows={8}
          />
        </Field>
        <ErrorMessage error={mutation.error} />
      </Dialog>
    </>
  )
}
