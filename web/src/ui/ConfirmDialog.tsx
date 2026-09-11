import type { ReactNode } from "react"
import Button from "@/ui/Button"
import Dialog from "@/ui/Dialog"
import ErrorMessage from "@/ui/ErrorMessage"

interface Props {
  /** Forwarded to the underlying <dialog> so callers drive showModal()/close(). */
  ref?: React.Ref<HTMLDialogElement> | undefined
  title: string
  /** Confirm button label. Default: "Confirm". */
  confirmLabel?: string | undefined
  onConfirm: () => void
  onClose: () => void
  /** True while the triggered mutation is in-flight. */
  isPending?: boolean | undefined
  /** Mutation error — shown inside the dialog so the user can retry. */
  error?: unknown
  children: ReactNode
}

/**
 * Destructive-action confirm modal: Dialog shell + danger confirm + cancel
 * buttons + inline ErrorMessage. Replaces window.confirm for mutations that
 * can fail (delete issue, revoke token, remove SSH key). Callers own the
 * ref (showModal/close), the mutation, and the success toast.
 */
export default function ConfirmDialog({
  ref,
  title,
  confirmLabel = "Confirm",
  onConfirm,
  onClose,
  isPending,
  error,
  children,
}: Props) {
  return (
    <Dialog
      ref={ref}
      title={title}
      onClose={onClose}
      footer={
        <>
          <ErrorMessage error={error} inline />
          <Button variant="ghost" onClick={onClose} disabled={isPending}>
            Cancel
          </Button>
          <Button variant="danger" onClick={onConfirm} disabled={isPending}>
            {isPending ? "Working…" : confirmLabel}
          </Button>
        </>
      }
    >
      {children}
    </Dialog>
  )
}
