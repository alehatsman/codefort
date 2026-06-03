import clsx from "clsx"
import { type ReactNode, useId } from "react"
import ErrorMessage from "@/ui/ErrorMessage"

/** What the render-prop child receives to wire onto its control. */
export interface FieldSlot {
  /** Put this on the control's `id` so the label's `htmlFor` matches. */
  controlId: string
  /** Put this on the control's `aria-describedby` (hint + error ids). */
  describedBy?: string
}

interface Props {
  /** The `.field__label` caption shown above the control. */
  label: ReactNode
  /**
   * The control. Pass a render-prop to receive {@link FieldSlot} (the ids to
   * spread onto the control), or a plain node when you wire the ids yourself.
   */
  children: ReactNode | ((slot: FieldSlot) => ReactNode)
  /**
   * `id` of the control the label points at. When omitted a stable id is
   * generated (`useId`) and handed to the render-prop child so the label,
   * hint, and error all associate with the control for assistive tech.
   */
  htmlFor?: string
  /** Muted helper text below the control (the `.field__hint`). */
  hint?: ReactNode
  /** Inline error rendered below the control (reuses `ErrorMessage inline`). */
  error?: unknown
  /** Extra class on the `.field` wrapper. */
  className?: string
}

/**
 * A labelled form field — the `.field` block with an explicit `htmlFor`/`id`
 * link to its control, optional `.field__hint` helper text, and an inline
 * error. Unlike {@link Field} (which wraps a `<label>` around the control),
 * this associates the label by id, so the control also gets an
 * `aria-describedby` pointing at the hint and error.
 *
 * Pass the control as a render-prop to wire the ids automatically:
 * `<FormField label="Name">{({ controlId, describedBy }) => (<Input id={controlId} aria-describedby={describedBy} />)}</FormField>`.
 * No cloning, no magic — the call site spreads the ids it's given.
 */
export default function FormField({ label, children, htmlFor, hint, error, className }: Props) {
  const generated = useId()
  const controlId = htmlFor ?? generated
  const hintId = hint != null ? `${controlId}-hint` : undefined
  const errorId = error ? `${controlId}-error` : undefined
  const describedBy = clsx(hintId, errorId) || undefined

  return (
    <div className={clsx("field", className)}>
      <label className="field__label" htmlFor={controlId}>
        {label}
      </label>
      {typeof children === "function" ? children({ controlId, describedBy }) : children}
      {hint != null && (
        <span className="field__hint" id={hintId}>
          {hint}
        </span>
      )}
      <ErrorMessage error={error} inline className="field__error" id={errorId} />
    </div>
  )
}
