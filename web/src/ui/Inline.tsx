import clsx from "clsx"
import type { ElementType, HTMLAttributes, ReactNode } from "react"
import type { SpaceStep } from "@/ui/Stack"

interface Props extends HTMLAttributes<HTMLElement> {
  /** Gap between children, in spacing-scale steps. Default 2 (8px). */
  gap?: SpaceStep
  /** Cross-axis alignment (align-items). Default "center". */
  align?: "start" | "center" | "end" | "baseline"
  /** Main-axis distribution (justify-content). Default "start". */
  justify?: "start" | "center" | "end" | "between"
  /** Allow items to wrap onto multiple lines. Default false. */
  wrap?: boolean
  /** Render as a different element (e.g. "ul", "header"). Default "div". */
  as?: ElementType
  className?: string
  children: ReactNode
}

/**
 * Horizontal flex container with token-driven gap — the row counterpart to
 * {@link Stack}. Use for toolbars-of-buttons, label+control pairs, and any
 * "things in a row" so inline spacing comes from one scale.
 */
export default function Inline({
  gap = 2,
  align = "center",
  justify = "start",
  wrap = false,
  as: Tag = "div",
  className,
  children,
  ...rest
}: Props) {
  return (
    <Tag
      className={clsx(
        "inline",
        `inline--gap-${gap}`,
        `inline--align-${align}`,
        `inline--justify-${justify}`,
        { "inline--wrap": wrap },
        className
      )}
      {...rest}
    >
      {children}
    </Tag>
  )
}
