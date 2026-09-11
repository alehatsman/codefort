import { useRef } from "react"
import { useNavigate } from "react-router-dom"
import { useDeleteIssue } from "@/api/mutations"
import { Button, ConfirmDialog } from "@/ui"

interface Props {
  owner: string
  repo: string
  number: number
}

export default function DeleteIssueButton({ owner, repo, number }: Props) {
  const navigate = useNavigate()
  const del = useDeleteIssue(owner, repo, number)
  const dialogRef = useRef<HTMLDialogElement>(null)

  function onConfirm() {
    del.mutate(undefined, {
      onSuccess: () => {
        dialogRef.current?.close()
        void navigate(`/${owner}/${repo}/issues`)
      },
    })
  }

  return (
    <>
      <Button
        variant="danger"
        disabled={del.isPending}
        onClick={() => dialogRef.current?.showModal()}
      >
        Delete issue
      </Button>
      <ConfirmDialog
        ref={dialogRef}
        title={`Delete issue #${number}?`}
        confirmLabel="Delete issue"
        onConfirm={onConfirm}
        onClose={() => dialogRef.current?.close()}
        isPending={del.isPending}
        error={del.error}
      >
        This permanently deletes the issue and all its comments. This cannot be undone.
      </ConfirmDialog>
    </>
  )
}
