import { useState } from "react"
import "./pulls.css"
import { useNavigate, useParams, useSearchParams } from "react-router-dom"
import { ApiError } from "@/api/client"
import { useCompare, useRefs } from "@/api/queries"
import { useCreatePull } from "@/api/mutations"
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
      { onSuccess: (pr) => navigate(`/${owner}/${repo}/pulls/${pr.number}`) }
    )
  }

  const sameBranch = base === head && head !== ""
  const canCreate = !!base && !!head && !sameBranch

  return (
    <div className="compare">
      <OverviewCard owner={owner} repo={repo} path="" summaries={{}} />

      <PageHeader title="Compare branches" />

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

      {sameBranch && <EmptyState>Pick two different branches to compare.</EmptyState>}
      {!head && !sameBranch && <EmptyState>Choose a head branch to compare.</EmptyState>}

      {compareQ.isLoading && <SkeletonText lines={4} />}
      {compareQ.error && <ErrorMessage error={compareQ.error} />}

      {compareQ.data && (
        <>
          <form className="compare__create" onSubmit={onCreate}>
            <input
              type="text"
              className="compare__title"
              placeholder={compareQ.data.commits[0]?.subject ?? "Pull request title"}
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

          <CompareView compare={compareQ.data} />
        </>
      )}
    </div>
  )
}
