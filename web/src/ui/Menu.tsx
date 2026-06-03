import clsx from "clsx"
import { type KeyboardEvent, type ReactNode, useEffect, useId, useRef, useState } from "react"

export interface MenuItem {
  label: ReactNode
  /** Invoked when the item is chosen (click or Enter/Space); the menu then closes. */
  onSelect: () => void
  disabled?: boolean
  /** Optional leading icon. */
  icon?: ReactNode
  /** Marks the current choice (e.g. the active theme) with a checked style. */
  selected?: boolean
}

interface Props {
  /** Accessible name for the menu (the trigger's label too). */
  label: string
  /** Trigger button content (text or an icon). */
  trigger: ReactNode
  items: MenuItem[]
  /** Which edge the popover aligns to. Default "start" (left). */
  align?: "start" | "end"
}

/**
 * A button-triggered action menu — `aria-haspopup` trigger + a `role="menu"`
 * popover of `role="menuitem"` buttons. Dependency-free: arrow keys rove focus
 * (skipping disabled, wrapping), Enter/Space select (native button click),
 * Escape and outside-click dismiss (Escape returns focus to the trigger). For
 * row actions, settings, a theme picker — a flat list of {label, onSelect}.
 */
export default function Menu({ label, trigger, items, align = "start" }: Props) {
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(0)
  const rootRef = useRef<HTMLDivElement>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)
  const itemRefs = useRef<(HTMLButtonElement | null)[]>([])
  const menuId = useId()

  function firstEnabled(): number {
    const i = items.findIndex((it) => !it.disabled)
    return i < 0 ? 0 : i
  }

  function openMenu() {
    setActive(firstEnabled())
    setOpen(true)
  }

  function close(focusTrigger = true) {
    setOpen(false)
    if (focusTrigger) triggerRef.current?.focus()
  }

  // Outside-click dismiss (no focus return — the click moved focus already).
  useEffect(() => {
    if (!open) return
    function onDown(e: MouseEvent) {
      if (!rootRef.current?.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener("mousedown", onDown)
    return () => document.removeEventListener("mousedown", onDown)
  }, [open])

  // Roving focus: move the DOM focus to the active item while open.
  useEffect(() => {
    if (open) itemRefs.current[active]?.focus()
  }, [open, active])

  function move(dir: 1 | -1) {
    setActive((cur) => {
      let i = cur
      for (let n = 0; n < items.length; n++) {
        i = (i + dir + items.length) % items.length
        if (!items[i]?.disabled) return i
      }
      return cur
    })
  }

  function onMenuKeyDown(e: KeyboardEvent) {
    if (e.key === "ArrowDown") {
      e.preventDefault()
      move(1)
    } else if (e.key === "ArrowUp") {
      e.preventDefault()
      move(-1)
    } else if (e.key === "Home") {
      e.preventDefault()
      setActive(firstEnabled())
    } else if (e.key === "Escape") {
      e.preventDefault()
      close()
    } else if (e.key === "Tab") {
      setOpen(false)
    }
  }

  function onTriggerKeyDown(e: KeyboardEvent) {
    if (e.key === "ArrowDown" || e.key === "Enter" || e.key === " ") {
      e.preventDefault()
      openMenu()
    }
  }

  function select(it: MenuItem) {
    if (it.disabled) return
    it.onSelect()
    close()
  }

  return (
    <div className="menu" ref={rootRef}>
      <button
        type="button"
        ref={triggerRef}
        className="btn menu__trigger"
        aria-label={label}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? menuId : undefined}
        onClick={() => (open ? setOpen(false) : openMenu())}
        onKeyDown={onTriggerKeyDown}
      >
        {trigger}
      </button>
      {open && (
        <div
          id={menuId}
          role="menu"
          aria-label={label}
          className={clsx("menu__popover", `menu__popover--${align}`)}
          onKeyDown={onMenuKeyDown}
        >
          {items.map((it, i) => (
            <button
              // biome-ignore lint/suspicious/noArrayIndexKey: a menu's items are a fixed, non-reordering list
              key={i}
              type="button"
              role="menuitem"
              ref={(el) => {
                itemRefs.current[i] = el
              }}
              className={clsx("menu__item", { "is-selected": it.selected })}
              disabled={it.disabled}
              tabIndex={i === active ? 0 : -1}
              onClick={() => select(it)}
            >
              {it.icon}
              {it.label}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}
