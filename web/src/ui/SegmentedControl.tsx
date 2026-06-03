import clsx from "clsx"
import type { ReactNode } from "react"

export interface SegmentedOption<T extends string> {
  value: T
  label: ReactNode
  /** Optional leading icon (e.g. a StateIcon). */
  icon?: ReactNode
  /** Extra class on this option's button — e.g. a per-state color modifier. */
  className?: string
}

interface Props<T extends string> {
  /** Accessible name for the control group. */
  label: string
  options: SegmentedOption<T>[]
  value: T
  onChange: (value: T) => void
  /** "column" (stacked, default) or "row" (joined horizontal segments). */
  orientation?: "row" | "column"
  /** Disables the whole control (e.g. while a mutation is in flight). */
  disabled?: boolean
  /** Locks the active option so it can't be re-selected (state-picker behavior). */
  lockActive?: boolean
}

/**
 * A pick-one control rendered as the `.segmented` block. The issue state picker
 * (column, colored per-state options, icons) and the diff Split/Unified toggle
 * (row, joined segments) were two separate BEM blocks for the same concept —
 * this is that concept, once. Per-option color/styling is supplied through each
 * option's `className`, so the primitive stays domain-agnostic; the `--row`
 * modifier carries the joined-segment look.
 */
export default function SegmentedControl<T extends string>({
  label,
  options,
  value,
  onChange,
  orientation = "column",
  disabled,
  lockActive,
}: Props<T>) {
  return (
    // biome-ignore lint/a11y/useSemanticElements: a labeled segmented group is a valid ARIA group; no native element fits
    <div
      className={clsx("segmented", { "segmented--row": orientation === "row" })}
      role="group"
      aria-label={label}
    >
      {options.map((opt) => {
        const active = opt.value === value
        return (
          <button
            key={opt.value}
            type="button"
            className={clsx("segmented__btn", opt.className, { "is-active": active })}
            aria-current={active ? "true" : undefined}
            disabled={disabled || (lockActive && active)}
            onClick={() => onChange(opt.value)}
          >
            {opt.icon}
            {opt.label}
          </button>
        )
      })}
    </div>
  )
}
