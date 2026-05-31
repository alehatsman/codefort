import clsx from "clsx"
import { useState } from "react"
import type { Compare } from "../api/types"
import DiffView from "./DiffView"

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
        {/* biome-ignore lint/a11y/useSemanticElements: a labeled segmented toggle is a valid ARIA group; no native element fits */}
        <div className="diff-summary__toggle" role="group" aria-label="Diff layout">
          <button
            type="button"
            className={clsx({ "is-active": mode === "split" })}
            aria-pressed={mode === "split"}
            onClick={() => setMode("split")}
          >
            Split
          </button>
          <button
            type="button"
            className={clsx({ "is-active": mode === "unified" })}
            aria-pressed={mode === "unified"}
            onClick={() => setMode("unified")}
          >
            Unified
          </button>
        </div>
      </div>

      {compare.truncated && (
        <div className="diff-truncated">
          This diff is large and was truncated; some changes are not shown.
        </div>
      )}

      {compare.files.length === 0 ? (
        <div className="empty">No file changes between these branches.</div>
      ) : (
        compare.files.map((f) => <DiffView key={f.new_path || f.old_path} file={f} mode={mode} />)
      )}
    </div>
  )
}
