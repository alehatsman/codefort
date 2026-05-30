import { Link } from "react-router-dom"
import type { Commit, TreeEntry } from "../api/types"
import FileIcon from "./FileIcon"
import { absoluteTime, timeAgo } from "../lib/timeAgo"

interface Props {
  owner: string
  repo: string
  entries: TreeEntry[]
  // Index of the keyboard-selected row, or -1 when nothing is selected.
  selectedIndex?: number
  // Last commit touching each entry, keyed by full path. Absent while the
  // tree-commits query is still loading (each row shows a placeholder).
  commits?: Record<string, Commit>
  // True while the commit annotations are loading, to drive the placeholder.
  commitsLoading?: boolean
}

/**
 * GitHub-style directory listing: folders first, then files, each a link.
 * Each row also carries the message + relative time of the last commit that
 * touched it (when available). Folders route to the tree view, files to the
 * blob view. The backend already sorts entries directories-first.
 */
export default function FileTree({
  owner,
  repo,
  entries,
  selectedIndex = -1,
  commits,
  commitsLoading = false,
}: Props) {
  const base = `/${owner}/${repo}`

  if (entries.length === 0) {
    return <div className="empty">This directory is empty.</div>
  }

  return (
    <ul className="file-tree">
      {entries.map((e, i) => {
        const kind = e.type === "tree" ? "tree" : "blob"
        const selected = i === selectedIndex
        const c = commits?.[e.path]
        return (
          <li
            key={e.path}
            className={`file-tree__row ${selected ? "is-vim-selected" : ""}`}
            data-vim-selected={selected ? "true" : undefined}
          >
            <Link to={`${base}/${kind}/${e.path}`} className="file-tree__link">
              <span className="file-tree__icon">
                <FileIcon type={e.type} />
              </span>
              <span className="file-tree__name">{e.name}</span>
            </Link>

            {c ? (
              <Link
                to={`${base}/commits/${e.path}`}
                className="file-tree__commit"
                title={c.subject}
              >
                {c.subject}
              </Link>
            ) : (
              <span className={`file-tree__commit file-tree__ph ${commitsLoading ? "is-loading" : ""}`} />
            )}

            {c ? (
              <span className="file-tree__age muted small" title={absoluteTime(c.date)}>
                {timeAgo(c.date)}
              </span>
            ) : (
              <span className={`file-tree__age file-tree__ph ${commitsLoading ? "is-loading" : ""}`} />
            )}
          </li>
        )
      })}
    </ul>
  )
}
