import clsx from "clsx"

interface Props {
  /**
   * The error to render. An `Error` shows its `.message`; anything else is
   * coerced with `String()`. Falsy (`null`/`undefined`/`false`) renders
   * nothing, so call sites drop the `{x.error && …}` guard.
   */
  error: unknown
  /** Inline variant (`.error.inline`) — sits within a form/section body. */
  inline?: boolean | undefined
  className?: string | undefined
  /** Optional id, e.g. so a control can reference it via `aria-describedby`. */
  id?: string | undefined
}

/**
 * The red error banner — the `.error` block. Replaces the
 * `<div className="error">{(x.error as Error).message}</div>` literal repeated
 * at ~35 call sites (query/mutation failure states), folding in the `inline`
 * modifier and the `Error → message` coercion.
 */
export default function ErrorMessage({ error, inline, className, id }: Props) {
  if (!error) return null
  const message = error instanceof Error ? error.message : String(error)
  return (
    <div className={clsx("error", { inline }, className)} id={id}>
      {message}
    </div>
  )
}
