import clsx from "clsx"
import type { ReactNode } from "react"
import { Link } from "react-router-dom"

interface TabsProps {
  /** Accessible name for the tab strip (e.g. "Repository navigation"). */
  label: string
  /** The `<Tab>` children. */
  children: ReactNode
}

/**
 * Horizontal tab strip — the `.tabs` block. A thin <nav> wrapper that pairs
 * with <Tab> children. The active tab is caller-derived: route matching is
 * specific to each strip (repo routes vs the global feed), so it stays in the
 * feature/shell component and the primitive owns only markup + class
 * composition. Replaces the hand-rolled `<nav className="tabs">` + per-tab
 * `clsx("tab", …)` duplicated in RepoTabs and GlobalTabs.
 */
export function Tabs({ label, children }: TabsProps) {
  return (
    <nav className="tabs" aria-label={label}>
      {children}
    </nav>
  )
}

interface TabProps {
  /** Router destination. */
  to: string
  /** Whether this tab matches the current route — caller-derived. */
  active?: boolean | undefined
  /** Optional count pill rendered after the label (e.g. open-issue count). */
  count?: number | undefined
  /** Renders the tab as a non-navigable `.is-disabled` span. */
  disabled?: boolean | undefined
  children: ReactNode
}

/**
 * A single tab — a `<Link className="tab">` with the `.is-active` modifier and
 * an optional `.tab__count` pill. When `disabled`, it renders as a non-link
 * span carrying `.is-disabled` so it shows but doesn't navigate.
 */
export function Tab({ to, active, count, disabled, children }: TabProps) {
  const className = clsx("tab", { "is-active": active, "is-disabled": disabled })
  const body = (
    <>
      {children}
      {count !== undefined && <span className="tab__count">{count}</span>}
    </>
  )
  if (disabled) {
    return (
      <span className={className} aria-disabled="true">
        {body}
      </span>
    )
  }
  return (
    <Link to={to} className={className}>
      {body}
    </Link>
  )
}
