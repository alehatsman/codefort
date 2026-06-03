import clsx from "clsx"

interface Props {
  name: string
  size?: "sm" | "lg"
  className?: string
}

/**
 * Initial-circle avatar — the `.avatar` block. No image fetching: the uppercase
 * first letter on a colored background, a GitHub-style placeholder until we have
 * real avatar storage. `name` is both the title and the accessible label.
 */
export default function Avatar({ name, size = "sm", className }: Props) {
  const initial = name.trim().charAt(0).toUpperCase() || "?"
  return (
    <span
      className={clsx("avatar", { "avatar--lg": size === "lg" }, className)}
      title={name}
      role="img"
      aria-label={name}
    >
      {initial}
    </span>
  )
}
