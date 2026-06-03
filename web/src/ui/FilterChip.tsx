import type { ReactNode } from "react"

interface Props {
  checked: boolean
  onChange: () => void
  /** The chip body — typically a state icon followed by its label. */
  children: ReactNode
}

/**
 * A checkbox styled as a rounded `.chip` — the state-filter toggle. The
 * identical `<label className="chip"><input type="checkbox" …/>…</label>`
 * markup lived in both IssuesPage and PullsPage; this is that block, once.
 */
const FilterChip = ({ checked, onChange, children }: Props) => {
  return (
    <label className="chip">
      <input type="checkbox" checked={checked} onChange={onChange} />
      {children}
    </label>
  )
}

export default FilterChip
