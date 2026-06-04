// Deterministic hue (0–359) from the repo key — same owner/repo always maps to
// the same hue across renders, reloads, and pages. Plain djb2-ish string hash
// spread over the color wheel.
export function repoHue(key: string): number {
  let h = 0
  for (let i = 0; i < key.length; i++) h = (h * 31 + key.charCodeAt(i)) >>> 0
  return h % 360
}
