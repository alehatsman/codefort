import { useRef, useState } from "react"
import { Button, Dialog, FormField } from "@/ui"
import "./issues.css"
import { useCreateBranch } from "@/api/mutations"

interface Props {
  owner: string
  repo: string
  issueNumber: number
  defaultBase?: string
  onCreated?: (name: string) => void
}

export default function CreateBranchDialog({ owner, repo, issueNumber, defaultBase, onCreated }: Props) {
  const dialogRef = useRef<HTMLDialogElement>(null)
  const [name, setName] = useState(`issue-${issueNumber}`)
  const [base, setBase] = useState(defaultBase ?? "")
  const [created, setCreated] = useState<{ name: string; sha: string } | null>(null)
  const mut = useCreateBranch(owner, repo)

  function open() {
    setName(`issue-${issueNumber}`)
    setBase(defaultBase ?? "")
    setCreated(null)
    mut.reset()
    dialogRef.current?.showModal()
  }

  function close() {
    dialogRef.current?.close()
  }

  function onSubmit(e: { preventDefault(): void }) {
    e.preventDefault()
    const trimmed = name.trim()
    if (!trimmed) return
    mut.mutate(
      { name: trimmed, base: base.trim() || undefined },
      {
        onSuccess(result) {
          setCreated(result)
          onCreated?.(result.name)
        },
      },
    )
  }

  return (
    <>
      <Button variant="ghost" size="small" type="button" onClick={open}>
        Create branch
      </Button>

      <Dialog
        ref={dialogRef}
        title="Create branch"
        onClose={close}
        onSubmit={created ? undefined : onSubmit}
        footer={
          created ? (
            <Button variant="primary" type="button" onClick={close}>
              Done
            </Button>
          ) : (
            <>
              <Button variant="ghost" type="button" onClick={close}>
                Cancel
              </Button>
              <Button variant="primary" type="submit" disabled={mut.isPending}>
                {mut.isPending ? "Creating…" : "Create branch"}
              </Button>
            </>
          )
        }
      >
        {created ? (
          <div className="create-branch-result">
            <p className="small">Branch created. To check it out:</p>
            <code className="create-branch-snippet">
              git fetch origin && git checkout {created.name}
            </code>
          </div>
        ) : (
          <>
            <FormField label="Branch name">
              <input
                className="input"
                value={name}
                onChange={(e) => setName(e.target.value)}
                autoFocus
                spellCheck={false}
              />
            </FormField>
            <FormField label="Base branch">
              <input
                className="input"
                value={base}
                onChange={(e) => setBase(e.target.value)}
                placeholder={defaultBase ?? "default branch"}
                spellCheck={false}
              />
            </FormField>
            {mut.isError && (
              <p className="muted small" style={{ color: "var(--color-danger)" }}>
                {(mut.error as Error).message}
              </p>
            )}
          </>
        )}
      </Dialog>
    </>
  )
}
