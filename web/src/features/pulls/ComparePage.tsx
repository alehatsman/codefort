import { useState } from "react"
import "./pulls.css"
import { useNavigate, useParams, useSearchParams } from "react-router-dom"
import { ApiError } from "@/api/client"
import { useCreatePull } from "@/api/mutations"
import { useCompare, useRefs } from "@/api/queries"
import CompareView from "@/features/pulls/CompareView"
import OverviewCard from "@/shell/OverviewCard"
import { Button, EmptyState, ErrorMessage, PageHeader, SkeletonText } from "@/ui"

/**
 * Compare two branches: pick base + head, see the ahead/behind + three-dot diff,
 * and open a pull request. base/head live in the URL (?base=&head=) so a
 * comparison is bookmarkable; base defaults to the repo's default branch.
 */
export default function ComparePage() {
  const { owner = "", repo = "" } = useParams()
  const navigate = useNavigate()
  const [params, setParams] = useSearchParams()

  const refsQ = useRefs(owner, repo)
  const branches = refsQ.data?.branches ?? []
  const def = refsQ.data?.default ?? ""

  const base = params.get("base") || def
  const head = params.get("head") || ""

  const compareQ = useCompare(owner, repo, base, head)
  const createPull = useCreatePull(owner, repo)

  const [title, setTitle] = useState("")
  const [body, setBody] = useState("")

  function setRef(key: "base" | "head", value: string) {
    setParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        if (value) next.set(key, value)
        else next.delete(key)
        return next
      },
      { replace: true }
    )
  }

  function onCreate(e: React.FormEvent) {
    e.preventDefault()
    const t = title.trim() || (compareQ.data?.commits[0]?.subject ?? "")
    if (!t) return
    createPull.mutate(
      { base, head, title: t, body: body.trim() || undefined },
      { onSuccess: (pr) => void navigate(`/${owner}/${repo}/pulls/${pr.number}`) }
    )
  }

  const sameBranch = base === head && head !== ""
  const canCreate = !!base && !!head && !sameBranch

  return (
    <div className="compare">
      <OverviewCard owner={owner} repo={repo} path="" />

      <PageHeader title="Compare branches" />

      <BranchPickers branches={branches} base={base} head={head} setRef={setRef} />

      {sameBranch && <EmptyState>Pick two different branches to compare.</EmptyState>}
      {!head && !sameBranch && <EmptyState>Choose a head branch to compare.</EmptyState>}

      {compareQ.isLoading && <SkeletonText lines={4} />}
      {compareQ.error && <ErrorMessage error={compareQ.error} />}

      {compareQ.data && (
        <>
          <CreatePullForm
            defaultTitle={compareQ.data.commits[0]?.subject ?? "Pull request title"}
            title={title}
            setTitle={setTitle}
            body={body}
            setBody={setBody}
            onSubmit={onCreate}
            canCreate={canCreate}
            createPull={createPull}
          />

          <CompareView compare={compareQ.data} />
        </>
      )}
    </div>
  )
}

function BranchPickers({
  branches,
  base,
  head,
  setRef,
}: {
  branches: string[]
  base: string
  head: string
  setRef: (key: "base" | "head", value: string) => void
}) {
  return (
    <div className="compare__pickers">
      <label className="filter-select">
        <span className="filter-label">base:</span>
        <select
          value={base}
          onChange={(e) => setRef("base", e.target.value)}
          aria-label="Base branch"
        >
          {branches.map((b) => (
            <option key={b} value={b}>
              {b}
            </option>
          ))}
        </select>
      </label>
      <span className="compare__arrow" aria-hidden="true">
        ←
      </span>
      <label className="filter-select">
        <span className="filter-label">head:</span>
        <select
          value={head}
          onChange={(e) => setRef("head", e.target.value)}
          aria-label="Head branch"
        >
          <option value="">choose a branch…</option>
          {branches.map((b) => (
            <option key={b} value={b}>
              {b}
            </option>
          ))}
        </select>
      </label>
    </div>
  )
}

function CreatePullForm({
  defaultTitle,
  title,
  setTitle,
  body,
  setBody,
  onSubmit,
  canCreate,
  createPull,
}: {
  defaultTitle: string
  title: string
  setTitle: (v: string) => void
  body: string
  setBody: (v: string) => void
  onSubmit: (e: React.FormEvent) => void
  canCreate: boolean
  createPull: ReturnType<typeof useCreatePull>
}) {
  return (
    <form className="compare__create" onSubmit={onSubmit}>
      <input
        type="text"
        className="compare__title"
        placeholder={defaultTitle}
        value={title}
        onChange={(e) => setTitle(e.target.value)}
        aria-label="Pull request title"
      />
      <textarea
        className="compare__body"
        placeholder="Description (optional)"
        value={body}
        onChange={(e) => setBody(e.target.value)}
        aria-label="Pull request description"
      />
      {createPull.error && (
        <div className="error inline">
          {createPull.error instanceof ApiError
            ? createPull.error.message
            : "Failed to create pull request."}
        </div>
      )}
      <Button type="submit" variant="primary" disabled={!canCreate || createPull.isPending}>
        {createPull.isPending ? "Creating…" : "Create pull request"}
      </Button>
    </form>
  )
}
