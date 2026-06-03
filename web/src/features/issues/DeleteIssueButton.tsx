import { useNavigate } from "react-router-dom"
import { useDeleteIssue } from "@/api/mutations"
import { Button, ErrorMessage } from "@/ui"

interface Props {
  owner: string
  repo: string
  number: number
}

/**
 * Destructive control that hard-deletes an issue and its comments. Guards
 * with window.confirm (matching CommentItem's posture), then navigates back
 * to the issues list on success; the mutation invalidates the list/repo
 * counts so they refresh.
 */
const DeleteIssueButton = ({ owner, repo, number }: Props) => {
  const navigate = useNavigate()
  const del = useDeleteIssue(owner, repo, number)

  function onDelete() {
    if (!confirm(`Delete issue #${number} and all its comments? This cannot be undone.`)) return
    del.mutate(undefined, {
      onSuccess: () => navigate(`/${owner}/${repo}/issues`),
    })
  }

  return (
    <>
      <Button variant="danger" disabled={del.isPending} onClick={onDelete}>
        {del.isPending ? "Deleting…" : "Delete issue"}
      </Button>
      {del.error && <ErrorMessage error={del.error} inline />}
    </>
  )
}

export default DeleteIssueButton
