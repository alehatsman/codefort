interface Props {
  name: string
  size?: "sm" | "lg"
}

/**
 * Initial-circle avatar. No image fetching — uppercase first letter on
 * a colored background, GitHub-style placeholder until we have real
 * avatar storage.
 */
export default function Avatar({ name, size = "sm" }: Props) {
  const initial = name.trim().charAt(0).toUpperCase() || "?"
  const cls = size === "lg" ? "avatar avatar--lg" : "avatar"
  return (
    <span className={cls} title={name} role="img" aria-label={name}>
      {initial}
    </span>
  )
}
