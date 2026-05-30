import { useEffect, useRef, useState } from "react"
import { useCreateIssue } from "../api/mutations"

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

  function onBackdropClick(e: React.MouseEvent<HTMLDialogElement>) {
    // Native <dialog> backdrop is the element itself when the click
    // target is dialog (the form below has stopPropagation via being
    // a child node). Closing on backdrop click matches platform
    // convention.
    if (e.target === dialogRef.current) close()
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
      <button type="button" className="btn btn--primary" onClick={open}>
        + New issue
      </button>

      <dialog ref={dialogRef} className="modal" onClick={onBackdropClick}>
        <form className="modal__form" onSubmit={submit}>
          <header className="modal__head">
            <h3 className="modal__title">New issue</h3>
            <button
              type="button"
              className="modal__close"
              onClick={close}
              aria-label="Close"
              title="Close"
            >
              ×
            </button>
          </header>

          <div className="modal__body">
            <label className="field">
              <span className="field__label">Title</span>
              <input
                ref={titleRef}
                className="input"
                placeholder="Short summary"
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                required
              />
            </label>
            <label className="field">
              <span className="field__label">Description</span>
              <textarea
                className="textarea"
                placeholder="Optional — what's the problem or task?"
                value={body}
                onChange={(e) => setBody(e.target.value)}
                rows={8}
              />
            </label>
            {mutation.error && <div className="error">{(mutation.error as Error).message}</div>}
          </div>

          <footer className="modal__foot">
            <button type="button" className="btn" onClick={close} disabled={mutation.isPending}>
              Cancel
            </button>
            <button
              type="submit"
              className="btn btn--primary"
              disabled={!title.trim() || mutation.isPending}
            >
              {mutation.isPending ? "Creating…" : "Submit new issue"}
            </button>
          </footer>
        </form>
      </dialog>
    </>
  )
}
