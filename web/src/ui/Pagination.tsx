import Button from "@/ui/Button"

interface Props {
  // 1-based current page.
  page: number
  pageSize: number
  // Full (unpaged) match count.
  total: number
  onPageChange: (page: number) => void
}

/**
 * Prev / next pager with a "showing X–Y of Z" range. Domain-agnostic: the
 * caller owns where `page` lives (URL, state) and supplies the total from the
 * server's X-Total-Count. Renders nothing when everything fits on one page.
 */
export default function Pagination({ page, pageSize, total, onPageChange }: Props) {
  const pageCount = Math.max(1, Math.ceil(total / pageSize))
  if (pageCount <= 1) return null

  const clamped = Math.min(Math.max(page, 1), pageCount)
  const first = (clamped - 1) * pageSize + 1
  const last = Math.min(clamped * pageSize, total)

  return (
    <nav className="pagination" aria-label="Pagination">
      <span className="pagination__range">
        {first}–{last} of {total}
      </span>
      <div className="pagination__controls">
        <Button
          size="small"
          variant="ghost"
          disabled={clamped <= 1}
          onClick={() => onPageChange(clamped - 1)}
        >
          ‹ Prev
        </Button>
        <span className="pagination__page">
          {clamped} / {pageCount}
        </span>
        <Button
          size="small"
          variant="ghost"
          disabled={clamped >= pageCount}
          onClick={() => onPageChange(clamped + 1)}
        >
          Next ›
        </Button>
      </div>
    </nav>
  )
}
