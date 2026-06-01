import { useState } from "react"
import { useUpdateIssue } from "../api/mutations"

interface Props {
  owner: string
  repo: string
  number: number
  initialTitle: string
  initialBody: string
  onDone: () => void
}

/**
 * Inline editor for an issue's title + body, seeded from the current values.
 * PATCHes both on save (the mutation invalidates the issue so the page
 * re-renders with the new content), then calls onDone to leave edit mode.
 * An empty body clears the description.
 */
export default function EditIssueForm({
  owner,
  repo,
  number,
  initialTitle,
  initialBody,
  onDone,
}: Props) {
  const [title, setTitle] = useState(initialTitle)
  const [body, setBody] = useState(initialBody)
  const mutation = useUpdateIssue(owner, repo, number)

  function submit(e: React.SyntheticEvent) {
    e.preventDefault()
    const trimmed = title.trim()
    if (!trimmed || mutation.isPending) return
    mutation.mutate({ title: trimmed, body }, { onSuccess: onDone })
  }

  return (
    <form className="issue-edit" onSubmit={submit}>
      <label className="field">
        <span className="field__label">Title</span>
        <input
          className="input"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          required
        />
      </label>
      <label className="field">
        <span className="field__label">Description</span>
        <textarea
          className="textarea"
          placeholder="Leave a description"
          value={body}
          onChange={(e) => setBody(e.target.value)}
          rows={8}
        />
      </label>
      {mutation.error && <div className="error">{(mutation.error as Error).message}</div>}
      <div className="row">
        <button
          type="submit"
          className="btn btn--primary"
          disabled={!title.trim() || mutation.isPending}
        >
          {mutation.isPending ? "Saving…" : "Save"}
        </button>
        <button type="button" className="btn btn--ghost" onClick={onDone}>
          Cancel
        </button>
      </div>
    </form>
  )
}
