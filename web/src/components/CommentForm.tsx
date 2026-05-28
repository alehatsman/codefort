import { useState } from "react"
import { useCreateComment } from "../api/mutations"

interface Props {
  owner: string
  repo: string
  number: number
}

/**
 * Add-a-comment textarea + submit button. Local state for the
 * draft; server state is the mutation.
 */
export default function CommentForm({ owner, repo, number }: Props) {
  const [body, setBody] = useState("")
  const mutation = useCreateComment(owner, repo, number)

  function submit(e: React.FormEvent) {
    e.preventDefault()
    const trimmed = body.trim()
    if (!trimmed || mutation.isPending) return
    mutation.mutate(
      { body: trimmed },
      {
        onSuccess: () => setBody(""),
      }
    )
  }

  return (
    <form className="comment-form" onSubmit={submit}>
      <textarea
        className="textarea"
        placeholder="Leave a comment"
        value={body}
        onChange={(e) => setBody(e.target.value)}
        rows={3}
      />
      {mutation.error && <div className="error">{(mutation.error as Error).message}</div>}
      <div className="row">
        <button
          type="submit"
          className="btn btn--primary"
          disabled={!body.trim() || mutation.isPending}
        >
          {mutation.isPending ? "Posting…" : "Comment"}
        </button>
      </div>
    </form>
  )
}
