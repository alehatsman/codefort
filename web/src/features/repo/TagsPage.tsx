import "./repo.css"
import { useParams } from "react-router-dom"
import { useRefs } from "@/api/queries"
import type { RepoTag } from "@/api/types"
import OverviewCard from "@/shell/OverviewCard"
import { EmptyState, ErrorMessage, Spinner } from "@/ui"
import { timeAgo } from "@/shell/timeAgo"

export default function TagsPage() {
  const { owner = "", repo = "" } = useParams()
  const refsQ = useRefs(owner, repo)

  const tags = refsQ.data?.tags ?? []

  return (
    <div className="repo">
      <OverviewCard owner={owner} repo={repo} path="" summaries={{}} />

      <div className="tags-page">
        <h2 className="tags-page__title">Tags</h2>

        {refsQ.isLoading && <Spinner />}
        <ErrorMessage error={refsQ.error} />

        {refsQ.data && tags.length === 0 && (
          <EmptyState>No tags in this repository yet.</EmptyState>
        )}

        {tags.length > 0 && (
          <ul className="tag-list">
            {tags.map((t) => (
              <TagRow key={t.name} owner={owner} repo={repo} tag={t} />
            ))}
          </ul>
        )}
      </div>
    </div>
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
