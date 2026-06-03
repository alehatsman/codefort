import type { ReactNode } from "react"

interface Props {
  /** Forwarded to the underlying `<dialog>` so callers drive `showModal()`/`close()`. */
  ref?: React.Ref<HTMLDialogElement>
  title: ReactNode
  /** Fired by the close button and a backdrop click. */
  onClose: () => void
  /** Submit handler for the wrapping `<form>` the dialog renders. */
  onSubmit?: (e: React.FormEvent) => void
  /** Footer content — typically the Cancel + submit buttons. */
  footer?: ReactNode
  /** The form body (Fields, ErrorMessage, helper copy). */
  children: ReactNode
}

/**
 * Modal dialog shell — the native `<dialog className="modal">` + `modal__form`
 * /`__head`/`__body`/`__foot` structure that was hand-copied in NewIssueForm,
 * NewRepoForm, and DraftReviewButton. Renders the backdrop-dismiss handler, the
 * titled header with its × close button, and the form wrapper; callers keep
 * their own `ref`, validation, and submit logic. `<dialog>` handles Esc and
 * focus natively.
 */
const Dialog = ({ ref, title, onClose, onSubmit, footer, children }: Props) => {
  function onBackdropClick(e: React.MouseEvent<HTMLDialogElement>) {
    if (e.target === e.currentTarget) onClose()
  }
  return (
    // biome-ignore lint/a11y/useKeyWithClickEvents: backdrop click-to-dismiss only; <dialog> handles Esc/keyboard natively
    <dialog ref={ref} className="modal" onClick={onBackdropClick}>
      <form className="modal__form" onSubmit={onSubmit}>
        <header className="modal__head">
          <h3 className="modal__title">{title}</h3>
          <button
            type="button"
            className="modal__close"
            onClick={onClose}
            aria-label="Close"
            title="Close"
          >
            ×
          </button>
        </header>

        <div className="modal__body">{children}</div>

        {footer && <footer className="modal__foot">{footer}</footer>}
      </form>
    </dialog>
  )
}

export default Dialog
