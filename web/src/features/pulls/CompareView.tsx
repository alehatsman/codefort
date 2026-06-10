import { useState } from "react"
import type { CodeComment, Compare } from "@/api/types"
import DiffView from "@/features/pulls/DiffView"
import { EmptyState, SegmentedControl } from "@/ui"

type Mode = "split" | "unified"

interface Props {
  compare: Compare
  comments?: CodeComment[]
  onAddComment?: (path: string, line: number, body: string) => Promise<void>
}

/**
 * The body of a branch comparison: an ahead/behind line, the list of commits
 * head introduces, a "N files changed" summary with a Split/Unified toggle, and
 * a DiffView per changed file. Shared by the standalone Compare screen and the
 * PR detail screen, which both render the three-dot diff identically.
 *
 * When comments + onAddComment are supplied (PR detail only), each DiffView
 * renders inline comment threads and a "+" affordance on changed lines.
 */
export default function CompareView({ compare, comments, onAddComment }: Props) {
  const [mode, setMode] = useState<Mode>("split")

  // Partition comments by file path so each DiffView only receives its own.
  const commentsByFile = new Map<string, CodeComment[]>()
  for (const c of comments ?? []) {
    const arr = commentsByFile.get(c.path) ?? []
    arr.push(c)
    commentsByFile.set(c.path, arr)
  }

  return (
    <div className="compare-view">
      <p className="compare-view__range muted small">
        <strong>{compare.head}</strong> is {compare.ahead} commit(s) ahead, {compare.behind} behind{" "}
        <strong>{compare.base}</strong>.
      </p>

      {compare.commits.length > 0 && (
        <ul className="compare-view__commits">
          {compare.commits.map((c) => (
            <li key={c.sha} className="compare-view__commit">
              <code className="compare-view__sha">{c.short_sha}</code>
              <span className="compare-view__subject">{c.subject}</span>
              <span className="compare-view__author muted small">{c.author}</span>
            </li>
          ))}
        </ul>
      )}

      <div className="diff-summary">
        <span className="diff-summary__counts">
          {compare.files.length} {compare.files.length === 1 ? "file" : "files"} changed
          {compare.additions > 0 && <span className="diff-file__add"> +{compare.additions}</span>}
          {compare.deletions > 0 && <span className="diff-file__del"> −{compare.deletions}</span>}
        </span>
        <SegmentedControl
          label="Diff layout"
          orientation="row"
          value={mode}
          onChange={setMode}
          options={[
            { value: "split", label: "Split" },
            { value: "unified", label: "Unified" },
          ]}
        />
      </div>

      {compare.truncated && (
        <div className="diff-truncated">
          This diff is large and was truncated; some changes are not shown.
        </div>
      )}

      {compare.files.length === 0 ? (
        <EmptyState>No file changes between these branches.</EmptyState>
      ) : (
        compare.files.map((f) => {
          const filePath = f.new_path || f.old_path
          return (
            <DiffView
              key={filePath}
              file={f}
              mode={mode}
              comments={commentsByFile.get(filePath)}
              onAddComment={onAddComment}
            />
          )
        })
      )}
    </div>
  )
}
