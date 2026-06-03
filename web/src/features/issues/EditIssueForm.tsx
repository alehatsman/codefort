import { useState } from "react"
import { useUpdateIssue } from "@/api/mutations"
import { Button, ErrorMessage, Field, Input, Textarea } from "@/ui"

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
const EditIssueForm = ({ owner, repo, number, initialTitle, initialBody, onDone }: Props) => {
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
      <Field label="Title">
        <Input value={title} onChange={(e) => setTitle(e.target.value)} required />
      </Field>
      <Field label="Description">
        <Textarea
          placeholder="Leave a description"
          value={body}
          onChange={(e) => setBody(e.target.value)}
          rows={8}
        />
      </Field>
      <ErrorMessage error={mutation.error} />
      <div className="row">
        <Button type="submit" variant="primary" disabled={!title.trim() || mutation.isPending}>
          {mutation.isPending ? "Saving…" : "Save"}
        </Button>
        <Button variant="ghost" onClick={onDone}>
          Cancel
        </Button>
      </div>
    </form>
  )
}

export default EditIssueForm
