// Color schemes. Each maps to a data-theme value consumed by styles.css.
// Persisted to localStorage; applied to <html> before first paint by the
// inline bootstrap in index.html, then kept in sync by ThemeSelect.

export const THEMES = [
  { id: "github", label: "GitHub" },
  { id: "monokai", label: "Monokai" },
] as const

export type ThemeId = (typeof THEMES)[number]["id"]

// What an unset preference renders as.
export const DEFAULT_THEME: ThemeId = "monokai"

// The scheme baked into styles.css with no overrides — rendered by leaving the
// data-theme attribute off. Every other theme layers on top via data-theme.
const BASE_THEME: ThemeId = "github"

const STORAGE_KEY = "moongit:theme"

function isThemeId(v: string | null): v is ThemeId {
  return THEMES.some((t) => t.id === v)
}

export function getTheme(): ThemeId {
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    // Legacy values from the earlier 3-scheme selector.
    if (stored === "monokai-web") return "monokai"
    if (stored === "monokai-code") return "github"
    if (isThemeId(stored)) return stored
  } catch {
    // localStorage unavailable (private mode, etc.) — fall through to default.
  }
  return DEFAULT_THEME
}

export function applyTheme(theme: ThemeId): void {
  // github is the base scheme (no overrides) — leave the attribute off.
  if (theme === BASE_THEME) {
    document.documentElement.removeAttribute("data-theme")
  } else {
    document.documentElement.setAttribute("data-theme", theme)
  }
}

export function setTheme(theme: ThemeId): void {
  try {
    localStorage.setItem(STORAGE_KEY, theme)
  } catch {
    // Persistence is best-effort; still apply for this session.
  }
  applyTheme(theme)
}
