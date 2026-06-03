import { useState } from "react"
import "./settings.css"
import { THEMES, getTheme, setTheme, type ThemeId } from "@/theme"

export default function ThemeSelect() {
  const [theme, setThemeState] = useState<ThemeId>(() => getTheme())

  function onChange(e: React.ChangeEvent<HTMLSelectElement>) {
    const next = e.target.value as ThemeId
    setTheme(next)
    setThemeState(next)
  }

  return (
    <label className="theme-select" title="Color scheme">
      <span className="theme-select__label">Theme</span>
      <select
        className="theme-select__input"
        value={theme}
        onChange={onChange}
        aria-label="Color scheme"
      >
        {THEMES.map((t) => (
          <option key={t.id} value={t.id}>
            {t.label}
          </option>
        ))}
      </select>
    </label>
  )
}
