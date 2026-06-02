import { useState } from "react"
import { useCreateComment } from "../api/mutations"
import { Button, Textarea } from "./ui"

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

  function submit(e: React.SyntheticEvent) {
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

  // Ctrl/Cmd+Enter posts the comment; plain Enter still inserts a newline.
  function onKeyDown(e: React.KeyboardEvent) {
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) submit(e)
  }

  return (
    <form className="comment-form" onSubmit={submit}>
      <Textarea
        placeholder="Leave a comment"
        value={body}
        onChange={(e) => setBody(e.target.value)}
        onKeyDown={onKeyDown}
        rows={3}
      />
      {mutation.error && <div className="error">{(mutation.error as Error).message}</div>}
      <div className="row">
        <Button type="submit" variant="primary" disabled={!body.trim() || mutation.isPending}>
          {mutation.isPending ? "Posting…" : "Comment"}
        </Button>
      </div>
    </form>
  )
}
