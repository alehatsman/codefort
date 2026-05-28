import { useRef, useState } from "react"
import { useCreateRepo } from "../api/mutations"
import { useWhoami } from "../api/queries"

interface Props {
  onCreated?: (owner: string, name: string) => void
}

const VALID = /^[A-Za-z0-9._-]+$/

/**
 * Modal dialog for provisioning a new repository. Mirrors NewIssueForm:
 * native <dialog> via showModal() for a real backdrop, Escape-to-close,
 * and focus trapping with no extra deps. State is local; the mutation
 * owns server state. Resets on close.
 */
export default function NewRepoForm({ onCreated }: Props) {
  const dialogRef = useRef<HTMLDialogElement>(null)
  const ownerRef = useRef<HTMLInputElement>(null)
  const whoami = useWhoami()
  const [owner, setOwner] = useState("")
  const [name, setName] = useState("")
  const mutation = useCreateRepo()

  function reset() {
    setOwner("")
    setName("")
    mutation.reset()
  }

  function open() {
    // Seed owner from the authenticated identity as a sensible default.
    setOwner((prev) => prev || whoami.data?.name || "")
    dialogRef.current?.showModal()
    queueMicrotask(() => ownerRef.current?.focus())
  }

  function close() {
    dialogRef.current?.close()
    reset()
  }

  function onBackdropClick(e: React.MouseEvent<HTMLDialogElement>) {
    if (e.target === dialogRef.current) close()
  }

  const trimmedOwner = owner.trim()
  const trimmedName = name.trim().replace(/\.git$/, "")
  const valid = VALID.test(trimmedOwner) && VALID.test(trimmedName)

  function submit(e: React.FormEvent) {
    e.preventDefault()
    if (!valid || mutation.isPending) return
    mutation.mutate(
      { owner: trimmedOwner, name: trimmedName },
      {
        onSuccess: (repo) => {
          close()
          onCreated?.(repo.owner, repo.name)
        },
      }
    )
  }

  return (
    <>
      <button className="btn btn--primary" onClick={open}>
        + New repo
      </button>

      <dialog ref={dialogRef} className="modal" onClick={onBackdropClick}>
        <form className="modal__form" onSubmit={submit}>
          <header className="modal__head">
            <h3 className="modal__title">New repository</h3>
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
              <span className="field__label">Owner</span>
              <input
                ref={ownerRef}
                className="input"
                placeholder="owner"
                value={owner}
                onChange={(e) => setOwner(e.target.value)}
                required
              />
            </label>
            <label className="field">
              <span className="field__label">Name</span>
              <input
                className="input"
                placeholder="repo-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                required
              />
            </label>
            <div className="field__label muted">
              Creates a bare git repo at <code>{trimmedOwner || "owner"}/{trimmedName || "name"}</code>.
              Allowed characters: letters, digits, <code>. _ -</code>
            </div>
            {mutation.error && <div className="error">{(mutation.error as Error).message}</div>}
          </div>

          <footer className="modal__foot">
            <button type="button" className="btn" onClick={close} disabled={mutation.isPending}>
              Cancel
            </button>
            <button type="submit" className="btn btn--primary" disabled={!valid || mutation.isPending}>
              {mutation.isPending ? "Creating…" : "Create repository"}
            </button>
          </footer>
        </form>
      </dialog>
    </>
  )
}
