import { Link } from "react-router-dom"
import type { TreeEntry } from "../api/types"
import FileIcon from "./FileIcon"

interface Props {
  owner: string
  repo: string
  entries: TreeEntry[]
  // Index of the keyboard-selected row, or -1 when nothing is selected.
  selectedIndex?: number
}

/**
 * GitHub-style directory listing: folders first, then files, each a link.
 * Folders route to the tree view, files to the blob view. The backend
 * already sorts entries directories-first.
 */
export default function FileTree({ owner, repo, entries, selectedIndex = -1 }: Props) {
  const base = `/${owner}/${repo}`

  if (entries.length === 0) {
    return <div className="empty">This directory is empty.</div>
  }

  return (
    <ul className="file-tree">
      {entries.map((e, i) => {
        const kind = e.type === "tree" ? "tree" : "blob"
        const selected = i === selectedIndex
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
          </li>
        )
      })}
    </ul>
  )
}
