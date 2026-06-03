/** The GitHub Primer-style status glyphs shared across the app's state icons. */
export type StatusGlyph =
  | "dot-ring" // open ring with a center dot — todo / queued
  | "clock" // ring + clock hand — in progress / running
  | "check" // ring + check — done / success
  | "x" // ring + × — closed / failed / error
  | "slash" // ring + slash — canceled
  | "alert" // ring + ! — stalled
  | "pause" // ring + ‖ — interrupted
  | "merge" // git-merge glyph (no ring) — a merged PR

// The outer ring shared by every circular glyph (everything but `merge`). Lived
// duplicated ~13 times across StateIcon / PRStateIcon / CIStatusIcon; now once.
const RING = "M8 0a8 8 0 1 1 0 16A8 8 0 0 1 8 0Zm0 1.5a6.5 6.5 0 1 0 0 13 6.5 6.5 0 0 0 0-13Z"

// Each glyph is the list of `<path d>` strings drawn inside the 0 0 16 16 box.
const GLYPHS: Record<StatusGlyph, string[]> = {
  "dot-ring": ["M8 9.5a1.5 1.5 0 1 0 0-3 1.5 1.5 0 0 0 0 3Z", RING],
  clock: [
    RING,
    "M8 4a.75.75 0 0 1 .75.75v3.19l2.03 2.03a.75.75 0 1 1-1.06 1.06L7.47 8.78A.75.75 0 0 1 7.25 8.25V4.75A.75.75 0 0 1 8 4Z",
  ],
  check: [
    RING,
    "M11.28 5.22a.75.75 0 0 1 0 1.06l-4 4a.75.75 0 0 1-1.06 0l-2-2a.75.75 0 1 1 1.06-1.06L6.75 8.69l3.47-3.47a.75.75 0 0 1 1.06 0Z",
  ],
  x: [
    RING,
    "M5.72 5.72a.75.75 0 0 1 1.06 0L8 6.94l1.22-1.22a.75.75 0 1 1 1.06 1.06L9.06 8l1.22 1.22a.75.75 0 1 1-1.06 1.06L8 9.06l-1.22 1.22a.75.75 0 1 1-1.06-1.06L6.94 8 5.72 6.78a.75.75 0 0 1 0-1.06Z",
  ],
  slash: [
    RING,
    "M4.97 4.97a.75.75 0 0 1 1.06 0l4.999 5a.75.75 0 0 1-1.06 1.06l-5-5a.75.75 0 0 1 0-1.06Z",
  ],
  alert: [
    RING,
    "M8 4a.75.75 0 0 1 .75.75v3.5a.75.75 0 0 1-1.5 0v-3.5A.75.75 0 0 1 8 4Zm0 5.5a1 1 0 1 1 0 2 1 1 0 0 1 0-2Z",
  ],
  pause: [
    RING,
    "M6.25 5a.75.75 0 0 1 .75.75v4.5a.75.75 0 0 1-1.5 0v-4.5A.75.75 0 0 1 6.25 5Zm3.5 0a.75.75 0 0 1 .75.75v4.5a.75.75 0 0 1-1.5 0v-4.5A.75.75 0 0 1 9.75 5Z",
  ],
  merge: [
    "M5.45 5.154A4.25 4.25 0 0 0 9.25 7.5h1.378a2.251 2.251 0 1 1 0 1.5H9.25A5.734 5.734 0 0 1 5 7.123v3.505a2.25 2.25 0 1 1-1.5 0V5.372a2.25 2.25 0 1 1 1.95-.218ZM4.25 13.5a.75.75 0 1 0 0-1.5.75.75 0 0 0 0 1.5Zm8.5-4.5a.75.75 0 1 0 0-1.5.75.75 0 0 0 0 1.5ZM5 3.25a.75.75 0 1 0-1.5 0 .75.75 0 0 0 1.5 0Z",
  ],
}

interface Props {
  /** Which glyph to draw. */
  glyph: StatusGlyph
  /** Accessible name + tooltip (e.g. "todo", "CI success", "merged"). */
  label: string
  size?: number
  /** Carries the color modifier (`state-icon--done`, `ci-icon--success`, …). */
  className?: string
}

/**
 * A status glyph in GitHub Primer style, drawn in a 16-unit viewBox with
 * `fill="currentColor"` — so color is driven entirely by the caller's
 * `className`. The glyph paths live here once; the domain icons (StateIcon for
 * issues, PRStateIcon for PRs, CIStatusIcon for runs) are thin maps from their
 * status to a `{glyph, colorClass}` pair over this primitive.
 */
export default function StatusIcon({ glyph, label, size = 16, className }: Props) {
  return (
    <svg
      className={className}
      width={size}
      height={size}
      viewBox="0 0 16 16"
      fill="currentColor"
      role="img"
      aria-label={label}
    >
      <title>{label}</title>
      {GLYPHS[glyph].map((d) => (
        <path key={d} d={d} />
      ))}
    </svg>
  )
}
