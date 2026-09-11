import { useState } from "react"
import { Link } from "react-router-dom"
import { useAddDependency, useRemoveDependency } from "@/api/mutations"
import type { IssueRef } from "@/api/types"
import StateIcon from "@/features/issues/StateIcon"
import { Button, SidebarSection } from "@/ui"

interface Props {
  owner: string
  repo: string
  issueNumber: number
  dependsOn?: IssueRef[] | undefined
  blocks?: IssueRef[] | undefined
  isActive: boolean
}

export default function DependencySection({
  owner,
  repo,
  issueNumber,
  dependsOn,
  blocks,
  isActive,
}: Props) {
  const [adding, setAdding] = useState(false)
  const [inputVal, setInputVal] = useState("")
  const addDep = useAddDependency(owner, repo, issueNumber)
  const removeDep = useRemoveDependency(owner, repo, issueNumber)

  function submitAdd(e: { preventDefault(): void }) {
    e.preventDefault()
    const n = Number(inputVal.replace(/^#/, "").trim())
    if (!n || n <= 0) return
    addDep.mutate(n, {
      onSuccess() {
        setInputVal("")
        setAdding(false)
      },
    })
  }

  const hasDeps = (dependsOn?.length ?? 0) > 0
  const hasBlocks = (blocks?.length ?? 0) > 0
  if (!hasDeps && !hasBlocks && !isActive) return null

  return (
    <SidebarSection label="Dependencies">
      {hasDeps && (
        <div className="dep-group">
          <span className="dep-group__label muted small">blocked by</span>
          <ul className="dep-list">
            {(dependsOn ?? []).map((ref) => (
              <DepRow
                key={ref.number}
                ref_={ref}
                owner={owner}
                repo={repo}
                canRemove={isActive}
                onRemove={() => removeDep.mutate(ref.number)}
              />
            ))}
          </ul>
        </div>
      )}
      {hasBlocks && (
        <div className="dep-group">
          <span className="dep-group__label muted small">blocks</span>
          <ul className="dep-list">
            {(blocks ?? []).map((ref) => (
              <DepRow
                key={ref.number}
                ref_={ref}
                owner={owner}
                repo={repo}
                canRemove={false}
                onRemove={() => {
                  // canRemove is false — the remove control isn't rendered, so this never fires.
                }}
              />
            ))}
          </ul>
        </div>
      )}
      {isActive &&
        (adding ? (
          <form className="dep-add-form" onSubmit={submitAdd}>
            <input
              className="input dep-add-input"
              value={inputVal}
              onChange={(e) => setInputVal(e.target.value)}
              placeholder="#issue"
              // biome-ignore lint/a11y/noAutofocus: user explicitly opened this form
              autoFocus
              spellCheck={false}
            />
            <Button variant="primary" size="small" type="submit" disabled={addDep.isPending}>
              Add
            </Button>
            <Button
              variant="ghost"
              size="small"
              type="button"
              onClick={() => {
                setAdding(false)
                setInputVal("")
              }}
            >
              Cancel
            </Button>
            {addDep.isError && (
              <span className="dep-add-error muted small">{(addDep.error as Error).message}</span>
            )}
          </form>
        ) : (
          <Button variant="ghost" size="small" type="button" onClick={() => setAdding(true)}>
            + Add dependency
          </Button>
        ))}
    </SidebarSection>
  )
}

function DepRow({
  ref_,
  owner,
  repo,
  canRemove,
  onRemove,
}: {
  ref_: IssueRef
  owner: string
  repo: string
  canRemove: boolean
  onRemove: () => void
}) {
  return (
    <li className="dep-row">
      <StateIcon state={ref_.state} size={12} />
      <Link
        to={`/${owner}/${repo}/issues/${ref_.number}`}
        className="dep-row__link small"
        title={ref_.title}
      >
        #{ref_.number} {ref_.title}
      </Link>
      {canRemove && (
        <button
          type="button"
          className="dep-row__remove"
          onClick={onRemove}
          title="Remove dependency"
        >
          ×
        </button>
      )}
    </li>
  )
}
