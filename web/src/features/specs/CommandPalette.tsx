import { useEffect, useMemo, useRef, useState } from "react"

/**
 * A spec workflow command. Either runs immediately, or — when it needs one
 * argument (e.g. a new spec's name) — collects it via `prompt` first. Phases
 * after this one register their commands (Verify, Draft, Plan→Issues, Score,
 * Ask) by adding entries to the array SpecsPage passes in; the palette itself
 * stays generic.
 */
export interface Command {
  id: string
  title: string
  subtitle?: string
  /** Display-only shortcut hint, e.g. "⌘P". */
  shortcut?: string
  run?: () => void
  prompt?: {
    placeholder: string
    onSubmit: (value: string) => void
  }
}

interface Props {
  open: boolean
  commands: Command[]
  onClose: () => void
}

/**
 * ⌘K command palette: the keyboard-first entry point to every spec workflow.
 * Built on the same native <dialog> shell as the other spec palettes. A command
 * either fires on Enter or, if it declares a `prompt`, switches the palette into
 * a single-field input (e.g. "New spec…" → type a name) before running.
 */
export default function CommandPalette({ open, commands, onClose }: Props) {
  const dialogRef = useRef<HTMLDialogElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const [query, setQuery] = useState("")
  const [active, setActive] = useState(0)
  // When set, the palette is collecting that command's argument.
  const [prompting, setPrompting] = useState<Command | null>(null)

  const matches = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return commands
    return commands.filter((c) => `${c.title} ${c.subtitle ?? ""}`.toLowerCase().includes(q))
  }, [commands, query])

  useEffect(() => {
    const d = dialogRef.current
    if (!d) return
    if (open && !d.open) {
      setQuery("")
      setActive(0)
      setPrompting(null)
      d.showModal()
      inputRef.current?.focus()
    } else if (!open && d.open) {
      d.close()
    }
  }, [open])

  // biome-ignore lint/correctness/useExhaustiveDependencies: reset highlight on query change
  useEffect(() => {
    setActive(0)
  }, [query])

  function choose(cmd: Command) {
    if (cmd.prompt) {
      setPrompting(cmd)
      setQuery("")
      setActive(0)
      inputRef.current?.focus()
      return
    }
    cmd.run?.()
    onClose()
  }

  function onKeyDown(e: React.KeyboardEvent) {
    if (prompting) {
      if (e.key === "Enter") {
        e.preventDefault()
        const v = query.trim()
        if (v) {
          prompting.prompt?.onSubmit(v)
          onClose()
        }
      }
      return
    }
    if (e.key === "ArrowDown") {
      e.preventDefault()
      setActive((i) => Math.min(i + 1, matches.length - 1))
    } else if (e.key === "ArrowUp") {
      e.preventDefault()
      setActive((i) => Math.max(i - 1, 0))
    } else if (e.key === "Enter") {
      e.preventDefault()
      const cmd = matches[active]
      if (cmd) choose(cmd)
    }
  }

  function onBackdropClick(e: React.MouseEvent<HTMLDialogElement>) {
    if (e.target === e.currentTarget) onClose()
  }

  return (
    // biome-ignore lint/a11y/useKeyWithClickEvents: backdrop click-to-dismiss only; <dialog> handles Esc/keyboard natively
    <dialog ref={dialogRef} className="quickopen cmdk" onClose={onClose} onClick={onBackdropClick}>
      <div className="quickopen__panel">
        <input
          ref={inputRef}
          className="quickopen__input"
          type="text"
          placeholder={prompting ? prompting.prompt?.placeholder : "Run a spec command…"}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={onKeyDown}
          aria-label={prompting ? prompting.title : "Run a spec command"}
        />
        {!prompting && (
          <ul className="cmdk__list">
            {matches.length === 0 ? (
              <li className="cmdk__empty muted small">No matching commands</li>
            ) : (
              matches.map((cmd, i) => (
                <li key={cmd.id}>
                  <button
                    type="button"
                    className={`cmdk__item${i === active ? " is-active" : ""}`}
                    data-active={i === active || undefined}
                    onMouseEnter={() => setActive(i)}
                    onClick={() => choose(cmd)}
                  >
                    <span className="cmdk__title">{cmd.title}</span>
                    {cmd.subtitle && (
                      <span className="cmdk__subtitle muted small">{cmd.subtitle}</span>
                    )}
                    {cmd.shortcut && <kbd className="cmdk__shortcut">{cmd.shortcut}</kbd>}
                  </button>
                </li>
              ))
            )}
          </ul>
        )}
      </div>
    </dialog>
  )
}
