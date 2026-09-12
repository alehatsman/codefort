import "./repo.css"
import { Link, useParams } from "react-router-dom"
import { useRefs } from "@/api/queries"
import type { RepoTag } from "@/api/types"
import OverviewCard from "@/shell/OverviewCard"
import { timeAgo } from "@/shell/timeAgo"
import { Badge, EmptyState, ErrorMessage, Spinner } from "@/ui"

export default function BranchesPage() {
  const { owner = "", repo = "" } = useParams()
  const refsQ = useRefs(owner, repo)

  const defaultBranch = refsQ.data?.default ?? ""
  const branches = refsQ.data?.branches ?? []
  const tags = refsQ.data?.tags ?? []

  // Default branch first, then alphabetical
  const sorted = [...branches].sort((a, b) => {
    if (a === defaultBranch) return -1
    if (b === defaultBranch) return 1
    return a.localeCompare(b)
  })

  return (
    <div className="repo">
      <OverviewCard owner={owner} repo={repo} path="" />

      <div className="branches-page">
        <h2 className="branches-page__title">Branches</h2>

        {refsQ.isLoading && <Spinner />}
        <ErrorMessage error={refsQ.error} />

        {refsQ.data && branches.length === 0 && (
          <EmptyState>No branches in this repository yet.</EmptyState>
        )}

        {sorted.length > 0 && (
          <ul className="branch-list">
            {sorted.map((name) => (
              <BranchRow
                key={name}
                owner={owner}
                repo={repo}
                name={name}
                isDefault={name === defaultBranch}
              />
            ))}
          </ul>
        )}

        {tags.length > 0 && (
          <>
            <h2 className="branches-page__title branches-page__title--tags">Tags</h2>
            <ul className="tag-list">
              {tags.map((t) => (
                <TagRow key={t.name} owner={owner} repo={repo} tag={t} />
              ))}
            </ul>
          </>
        )}
      </div>
    </div>
  )
}

function BranchRow({
  owner,
  repo,
  name,
  isDefault,
}: {
  owner: string
  repo: string
  name: string
  isDefault: boolean
}) {
  return (
    <li className="branch-row">
      <div className="branch-row__name">
        <Link to={`/${owner}/${repo}/tree/${name}`} className="branch-row__link">
          {name}
        </Link>
        {isDefault && <Badge className="branch-row__badge">default</Badge>}
      </div>
      <div className="branch-row__actions">
        <Link
          to={`/${owner}/${repo}/compare?head=${encodeURIComponent(name)}`}
          className="branch-row__compare muted small"
        >
          Compare
        </Link>
      </div>
    </li>
  )
}

function TagRow({ owner, repo, tag }: { owner: string; repo: string; tag: RepoTag }) {
  return (
    <li className="tag-row">
      <div className="tag-row__main">
        <a className="tag-row__name" href={`/${owner}/${repo}/tree/${tag.name}`}>
          {tag.name}
        </a>
        {tag.message && <span className="tag-row__message muted">{tag.message}</span>}
      </div>
      <div className="tag-row__meta muted small">
        <code className="tag-row__sha">{tag.sha.slice(0, 7)}</code>
        {tag.created_at && <span title={tag.created_at}>{timeAgo(tag.created_at)}</span>}
      </div>
    </li>
  )
}
