import { Link } from "react-router-dom"

interface Props {
  title?: string
  detail?: string
}

// Styled fallback for unmatched routes and 404s from the API — used instead of
// a blank page or a raw backend error string.
export default function NotFound({ title = "Page not found", detail }: Props) {
  return (
    <div className="empty">
      <h2>{title}</h2>
      {detail && <p className="muted small">{detail}</p>}
      <p>
        <Link to="/">← Back to repositories</Link>
      </p>
    </div>
  )
}
