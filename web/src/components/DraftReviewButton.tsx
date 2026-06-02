import { useEffect, useRef, useState } from "react"
import { useNavigate } from "react-router-dom"
import { useDraftReviewAgent } from "../api/mutations"
import {
  type ReviewTarget,
  draftReviewIssue,
  reviewTargetNeedsBase,
  reviewTargetNeedsPath,
} from "../lib/reviewTemplates"

interface Props {
  owner: string
  repo: string
  // defaultRef seeds the ref field — the branch the Review tab is showing.
  defaultRef: string
}

const TARGETS: { value: ReviewTarget; label: string }[] = [
  { value: "diff", label: "Branch / PR diff" },
  { value: "commit", label: "Single commit" },
  { value: "file", label: "A file" },
  { value: "package", label: "A package / directory" },
]

/**
 * "Draft review issue → spawn agent" on the Review tab (#159). Opens a modal to
 * pick one of the four review targets (diff / commit / file / package) plus its
 * ref/path, renders an issue title+body from the shared templates, creates the
 * issue, and spawns a read-only review agent (toolProfile "review") against it.
 * The agent's findings then surface live in this same Review tab (the
 * codeComments query invalidates on each review_create write). On success we
 * navigate to the agent run's transcript under Pipelines.
 */
export default function DraftReviewButton({ owner, repo, defaultRef }: Props) {
  const navigate = useNavigate()
  const dialogRef = useRef<HTMLDialogElement>(null)
  const draft = useDraftReviewAgent(owner, repo)

  const [target, setTarget] = useState<ReviewTarget>("diff")
  const [ref, setRef] = useState(defaultRef)
  const [base, setBase] = useState("")
  const [path, setPath] = useState("")

  // Reset the form whenever the dialog closes, and reseed ref from the tab.
  useEffect(() => {
    const dialog = dialogRef.current
    if (!dialog) return
    const reset = () => {
      setTarget("diff")
      setRef(defaultRef)
      setBase("")
      setPath("")
      draft.reset()
    }
    dialog.addEventListener("close", reset)
    return () => dialog.removeEventListener("close", reset)
  }, [draft.reset, defaultRef])

  function open() {
    setRef(defaultRef)
    dialogRef.current?.showModal()
  }
  function close() {
    dialogRef.current?.close()
  }
  function onBackdropClick(e: React.MouseEvent<HTMLDialogElement>) {
    if (e.target === dialogRef.current) close()
  }

  const needsPath = reviewTargetNeedsPath(target)
  const needsBase = reviewTargetNeedsBase(target)
  const ready =
    ref.trim() !== "" && (!needsPath || path.trim() !== "") && (!needsBase || base.trim() !== "")

  function submit(e: React.FormEvent) {
    e.preventDefault()
    if (!ready || draft.isPending) return
    const issue = draftReviewIssue({ target, ref, base, path })
    draft.mutate(
      { title: issue.title, body: issue.body },
      { onSuccess: ({ run }) => navigate(`/${owner}/${repo}/agents/${run.number}`) }
    )
  }

  return (
    <>
      <button type="button" className="btn" onClick={open}>
        Draft review issue
      </button>

      {/* biome-ignore lint/a11y/useKeyWithClickEvents: backdrop click-to-dismiss only; <dialog> handles Esc/keyboard natively */}
      <dialog ref={dialogRef} className="modal" onClick={onBackdropClick}>
        <form className="modal__form" onSubmit={submit}>
          <header className="modal__head">
            <h3 className="modal__title">Draft review issue</h3>
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
              <span className="field__label">Target</span>
              <select
                className="select"
                value={target}
                onChange={(e) => setTarget(e.target.value as ReviewTarget)}
              >
                {TARGETS.map((t) => (
                  <option key={t.value} value={t.value}>
                    {t.label}
                  </option>
                ))}
              </select>
            </label>

            <label className="field">
              <span className="field__label">{target === "commit" ? "Commit" : "Ref"}</span>
              <input
                className="input"
                placeholder={target === "commit" ? "commit SHA" : "branch / tag / SHA"}
                value={ref}
                onChange={(e) => setRef(e.target.value)}
                required
              />
            </label>

            {needsBase && (
              <label className="field">
                <span className="field__label">Base ref</span>
                <input
                  className="input"
                  placeholder="base branch (diff is base..ref)"
                  value={base}
                  onChange={(e) => setBase(e.target.value)}
                  required
                />
              </label>
            )}

            {needsPath && (
              <label className="field">
                <span className="field__label">
                  {target === "file" ? "File path" : "Directory"}
                </span>
                <input
                  className="input"
                  placeholder={target === "file" ? "path/to/file.go" : "path/to/dir"}
                  value={path}
                  onChange={(e) => setPath(e.target.value)}
                  required
                />
              </label>
            )}

            <p className="muted small">
              Creates an issue and spawns a read-only review agent (read + review tools only). Its
              findings appear here as you watch the transcript under Pipelines.
            </p>
            {draft.error && <div className="error">{(draft.error as Error).message}</div>}
          </div>

          <footer className="modal__foot">
            <button type="button" className="btn" onClick={close} disabled={draft.isPending}>
              Cancel
            </button>
            <button type="submit" className="btn btn--primary" disabled={!ready || draft.isPending}>
              {draft.isPending ? "Spawning…" : "Create + spawn review agent"}
            </button>
          </footer>
        </form>
      </dialog>
    </>
  )
}
