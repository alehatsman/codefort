import { useState } from "react"
import { useUpdateIssue } from "@/api/mutations"
import { Button, ErrorMessage, Field, Input, Textarea } from "@/ui"

interface Props {
  owner: string
  repo: string
  number: number
  initialTitle: string
  initialBody: string
  initialLabels: string[]
  onDone: () => void
}

/**
 * Inline editor for an issue's title + body + labels, seeded from the current
 * values. PATCHes on save (the mutation invalidates the issue so the page
 * re-renders with the new content), then calls onDone to leave edit mode.
 * An empty body clears the description; an empty labels string clears all labels.
 */
export default function EditIssueForm({
  owner,
  repo,
  number,
  initialTitle,
  initialBody,
  initialLabels,
  onDone,
}: Props) {
  const [title, setTitle] = useState(initialTitle)
  const [body, setBody] = useState(initialBody)
  const [labelsStr, setLabelsStr] = useState(initialLabels.join(", "))
  const mutation = useUpdateIssue(owner, repo, number)

  function submit(e: React.SyntheticEvent) {
    e.preventDefault()
    const trimmed = title.trim()
    if (!trimmed || mutation.isPending) return
    const labels = labelsStr
      .split(",")
      .map((l) => l.trim())
      .filter(Boolean)
    mutation.mutate({ title: trimmed, body, labels }, { onSuccess: onDone })
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
      <Field label="Labels">
        <Input
          placeholder="bug, ui, backend (comma-separated)"
          value={labelsStr}
          onChange={(e) => setLabelsStr(e.target.value)}
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
