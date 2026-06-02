// Helpers for resolving relative links/images inside rendered markdown to
// repo-relative paths, mirroring how GitHub rewrites README references.

/**
 * True for refs that point outside the repo tree and should be left as-is:
 * anything with a URL scheme (http:, https:, mailto:, data:) or a
 * protocol-relative `//host` prefix. In-page anchors (`#...`) are handled
 * separately by the caller, so they are NOT treated as external here.
 */
export function isExternalRef(ref: string): boolean {
  return /^[a-z][a-z0-9+.-]*:/i.test(ref) || ref.startsWith("//")
}

/**
 * Resolve a relative link/image target to a clean repo-relative path.
 * `basePath` is the directory the markdown file lives in ("" at repo root).
 * A leading "/" is repo-root-relative (GitHub semantics). Any query/hash
 * suffix is dropped, and `..` segments that climb past the root are clamped.
 */
export function resolveRepoPath(basePath: string, target: string): string {
  const cut = target.search(/[?#]/)
  const clean = cut >= 0 ? target.slice(0, cut) : target

  const segments = clean.startsWith("/")
    ? clean.slice(1).split("/")
    : (basePath ? basePath.split("/") : []).concat(clean.split("/"))

  const out: string[] = []
  for (const seg of segments) {
    if (seg === "" || seg === ".") continue
    if (seg === "..") out.pop()
    else out.push(seg)
  }
  return out.join("/")
}
