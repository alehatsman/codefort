import { Link } from "react-router-dom"
import { EmptyState } from "@/ui"

interface Props {
  title?: string
  detail?: string
}

// Styled fallback for unmatched routes and 404s from the API — used instead of
// a blank page or a raw backend error string.
const NotFound = ({ title = "Page not found", detail }: Props) => {
  return (
    <EmptyState>
      <h2>{title}</h2>
      {detail && <p className="muted small">{detail}</p>}
      <p>
        <Link to="/">← Back to repositories</Link>
      </p>
    </EmptyState>
  )
}

export default NotFound
