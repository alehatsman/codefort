import { useEffect, useRef, useState } from "react"
import { useMutation } from "@tanstack/react-query"
import { api } from "@/api/client"
import { ErrorMessage } from "@/ui"
import type { SpecSearchHit, SpecSearchResult } from "@/api/types"

interface Props {
  open: boolean
  owner: string
  repo: string
  onPick: (path: string, section: string) => void
  onClose: () => void
}

/**
 * Semantic spec search (⌘⇧F): a palette that queries the dex-backed
 * .../specs/search endpoint and lists matches as section + snippet. Picking a
 * result opens the spec and deep-links to the matched section. The spec corpus
 * is the differentiator — this is "ask the specs", distinct from the ⌘P
 * path-jump (QuickOpen).
 */
export default function SpecSearch({ open, owner, repo, onPick, onClose }: Props) {
  const dialogRef = useRef<HTMLDialogElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const [query, setQuery] = useState("")
  const search = useMutation<SpecSearchResult, Error, string>({
    mutationFn: (q) => api.searchSpecs(owner, repo, q),
  })

  // The mutation object's identity churns with its state; hold reset in a ref so
  // the open effect can depend only on `open`.
  const resetRef = useRef(search.reset)
  resetRef.current = search.reset

  useEffect(() => {
    const d = dialogRef.current
    if (!d) return
    if (open && !d.open) {
      setQuery("")
      resetRef.current()
      d.showModal()
      inputRef.current?.focus()
    } else if (!open && d.open) {
      d.close()
    }
  }, [open])

  function onSubmit(e: React.FormEvent) {
    e.preventDefault()
    const q = query.trim()
    if (!q || search.isPending) return
    search.mutate(q)
  }

  function pick(hit: SpecSearchHit) {
    onPick(hit.path, hit.section ?? "")
    onClose()
  }

  const hits = search.data?.hits ?? []

  function onBackdropClick(e: React.MouseEvent<HTMLDialogElement>) {
    if (e.target === e.currentTarget) onClose()
  }

  return (
    // biome-ignore lint/a11y/useKeyWithClickEvents: backdrop click-to-dismiss only; <dialog> handles Esc/keyboard natively
    <dialog
      ref={dialogRef}
      className="quickopen specsearch"
      onClose={onClose}
      onClick={onBackdropClick}
    >
      <div className="quickopen__panel">
        <form onSubmit={onSubmit}>
          <input
            ref={inputRef}
            className="quickopen__input"
            type="text"
            placeholder="Search specs semantically…"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            aria-label="Search specs"
          />
        </form>

        <ErrorMessage error={search.error} />

        {search.isPending && <div className="specsearch__status muted small">Searching…</div>}

        {search.data && hits.length === 0 && (
          <div className="specsearch__status muted small">No matching specs.</div>
        )}

        {hits.length > 0 && (
          <ul className="specsearch__results">
            {hits.map((h) => (
              <li key={`${h.path}:${h.line}`}>
                <button type="button" className="specsearch__hit" onClick={() => pick(h)}>
                  <span className="specsearch__hit-head">
                    {h.section && <span className="specsearch__section">{h.section}</span>}
                    <span className="specsearch__path muted small">{h.path}</span>
                  </span>
                  {h.snippet && <span className="specsearch__snippet">{h.snippet}</span>}
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </dialog>
  )
}
