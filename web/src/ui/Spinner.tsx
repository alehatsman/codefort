interface Props {
  /** Override the default "Loading…" copy. */
  label?: string
}

/**
 * The standard loading placeholder — the `.loading` block. Replaces the
 * `<div className="loading">Loading…</div>` literal that was repeated at ~10
 * call sites (route-level query-pending states). Centralizes the copy and the
 * ellipsis character so they can't drift.
 */
const Spinner = ({ label = "Loading…" }: Props) => {
  return <div className="loading">{label}</div>
}

export default Spinner
