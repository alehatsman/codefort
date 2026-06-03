import clsx from "clsx"

interface Props {
  branch: string
  className?: string
}

/**
 * Small pill labeling which branch a commit is on — the server's "primary"
 * branch: the default branch if it contains the commit, else the first branch
 * that does. Lossy by design (a commit can live on several branches), so this
 * is a hint, not the full set.
 */
export default function BranchTag({ branch, className }: Props) {
  return (
    <span className={clsx("branch-tag", className)} title={`on ${branch}`}>
      <svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true" fill="currentColor">
        <path d="M9.5 3.25a2.25 2.25 0 1 1 3 2.122V6A2.5 2.5 0 0 1 10 8.5H6a1 1 0 0 0-1 1v1.128a2.251 2.251 0 1 1-1.5 0V5.372a2.25 2.25 0 1 1 1.5 0v1.836A2.493 2.493 0 0 1 6 7h4a1 1 0 0 0 1-1v-.628A2.25 2.25 0 0 1 9.5 3.25Zm-6 0a.75.75 0 1 0 1.5 0 .75.75 0 0 0-1.5 0Zm8.25-.75a.75.75 0 1 0 0 1.5.75.75 0 0 0 0-1.5ZM4.25 12a.75.75 0 1 0 0 1.5.75.75 0 0 0 0-1.5Z" />
      </svg>
      {branch}
    </span>
  )
}
