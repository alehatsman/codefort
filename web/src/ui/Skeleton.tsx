import clsx from "clsx"
import type { CSSProperties } from "react"

interface Props {
  /** Shape: a text "line" (default), a "block" (give it a height), or a "circle" (avatar). */
  variant?: "line" | "block" | "circle"
  /** Override width — number is px, string is used as-is. */
  width?: string | number
  /** Override height — number is px, string is used as-is. */
  height?: string | number
  className?: string
}

function dim(v: string | number | undefined): string | undefined {
  if (v == null) return undefined
  return typeof v === "number" ? `${v}px` : v
}

/**
 * A shimmering loading placeholder — the `.skeleton` block. Use to reserve a
 * list/detail layout's shape while data loads so the page doesn't jump (where
 * {@link Spinner} just spins). Decorative, so it's `aria-hidden`; announce the
 * loading state elsewhere (e.g. a Spinner label or an aria-live region).
 */
export default function Skeleton({ variant = "line", width, height, className }: Props) {
  const style: CSSProperties = { width: dim(width), height: dim(height) }
  return (
    <span
      className={clsx("skeleton", `skeleton--${variant}`, className)}
      style={style}
      aria-hidden="true"
    />
  )
}
