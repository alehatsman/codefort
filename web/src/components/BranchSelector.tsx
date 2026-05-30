import { useSearchParams } from "react-router-dom"
import { useRefs } from "../api/queries"

interface Props {
  owner: string
  repo: string
}

/**
 * Branch picker for the code browser. The selected branch lives in the `?ref=`
 * search param so it threads through the blob/tree/commits queries and the code
 * comments anchored to it. An empty value means the repo's default branch — we
 * never write the default into the URL, keeping default-branch links clean.
 */
export default function BranchSelector({ owner, repo }: Props) {
  const refsQ = useRefs(owner, repo)
  const [params, setParams] = useSearchParams()
  const current = params.get("ref") ?? ""

  const branches = refsQ.data?.branches ?? []
  const def = refsQ.data?.default ?? ""
  if (branches.length === 0) return null

  function onChange(e: React.ChangeEvent<HTMLSelectElement>) {
    const next = new URLSearchParams(params)
    if (e.target.value === "" || e.target.value === def) {
      next.delete("ref")
    } else {
      next.set("ref", e.target.value)
    }
    setParams(next, { replace: true })
  }

  return (
    <label className="branch-select" title="Switch branch">
      <svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true" fill="currentColor">
        <path d="M9.5 3.25a2.25 2.25 0 1 1 3 2.122V6A2.5 2.5 0 0 1 10 8.5H6a1 1 0 0 0-1 1v1.128a2.251 2.251 0 1 1-1.5 0V5.372a2.25 2.25 0 1 1 1.5 0v1.836A2.493 2.493 0 0 1 6 7h4a1 1 0 0 0 1-1v-.628A2.25 2.25 0 0 1 9.5 3.25Zm-6 0a.75.75 0 1 0 1.5 0 .75.75 0 0 0-1.5 0Zm8.25-.75a.75.75 0 1 0 0 1.5.75.75 0 0 0 0-1.5ZM4.25 12a.75.75 0 1 0 0 1.5.75.75 0 0 0 0-1.5Z" />
      </svg>
      <select value={current || def} onChange={onChange} aria-label="Branch">
        {branches.map((b) => (
          <option key={b} value={b}>
            {b}
          </option>
        ))}
      </select>
    </label>
  )
}
