import clsx from "clsx"
import type { ElementType, HTMLAttributes, ReactNode } from "react"

/** Spacing-scale step (`--space-1`…`--space-6`); `0` is no gap. */
export type SpaceStep = 0 | 1 | 2 | 3 | 4 | 5 | 6

interface Props extends HTMLAttributes<HTMLElement> {
  /** Gap between children, in spacing-scale steps. Default 4 (16px). */
  gap?: SpaceStep
  /** Cross-axis alignment (align-items). Default "stretch". */
  align?: "start" | "center" | "end" | "stretch"
  /** Render as a different element (e.g. "ul", "section"). Default "div". */
  as?: ElementType
  className?: string
  children: ReactNode
}

/**
 * Vertical flex container with token-driven gap — the building block for page
 * rhythm. Use instead of ad-hoc `display:flex;flex-direction:column` wrappers so
 * vertical spacing comes from one scale. Pair with {@link Inline} for rows.
 */
export default function Stack({
  gap = 4,
  align = "stretch",
  as: Tag = "div",
  className,
  children,
  ...rest
}: Props) {
  return (
    <Tag
      className={clsx("stack", `stack--gap-${gap}`, `stack--align-${align}`, className)}
      {...rest}
    >
      {children}
    </Tag>
  )
}
