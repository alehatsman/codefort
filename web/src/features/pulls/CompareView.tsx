import { useState } from "react"
import type { Compare } from "@/api/types"
import DiffView from "@/features/pulls/DiffView"
import { EmptyState, SegmentedControl } from "@/ui"

type Mode = "split" | "unified"

interface Props {
  compare: Compare
}

/**
 * The body of a branch comparison: an ahead/behind line, the list of commits
 * head introduces, a "N files changed" summary with a Split/Unified toggle, and
 * a DiffView per changed file. Shared by the standalone Compare screen and the
 * PR detail screen, which both render the three-dot diff identically.
 */
export default function CompareView({ compare }: Props) {
  const [mode, setMode] = useState<Mode>("split")

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
        compare.files.map((f) => <DiffView key={f.new_path || f.old_path} file={f} mode={mode} />)
      )}
    </div>
  )
}
