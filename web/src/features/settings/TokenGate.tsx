import { useState } from "react"
import "./settings.css"
import { setToken, verifyToken } from "@/api/client"
import { ErrorMessage } from "@/ui"

interface Props {
  onSet: () => void
}

export default function TokenGate({ onSet }: Props) {
  const [value, setValue] = useState("")
  const [error, setError] = useState<string | null>(null)
  const [checking, setChecking] = useState(false)

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    const trimmed = value.trim()
    if (!trimmed.startsWith("mgt_")) {
      setError("Tokens start with mgt_")
      return
    }
    setError(null)
    setChecking(true)
    // Validate against the server before persisting, so an invalid token never
    // lands in localStorage and traps the user in a broken authenticated shell.
    const ok = await verifyToken(trimmed)
    if (!ok) {
      setChecking(false)
      setError("That token was rejected. Check it and try again.")
      return
    }
    setToken(trimmed)
    onSet()
  }

  return (
    <div className="gate">
      <div className="gate__card">
        <h1>moongit</h1>
        <p className="muted">
          Paste an API token to continue. Mint one with{" "}
          <code>moongitd token create &lt;name&gt;</code>.
        </p>
        <form onSubmit={submit}>
          <input
            type="password"
            placeholder="mgt_..."
            value={value}
            onChange={(e) => setValue(e.target.value)}
            disabled={checking}
            // biome-ignore lint/a11y/noAutofocus: full-screen pre-auth gate with one field — focusing it on load is the expected flow, nothing to skip past.
            autoFocus
          />
          <ErrorMessage error={error} />
          <button type="submit" disabled={checking}>
            {checking ? "Checking…" : "Continue"}
          </button>
        </form>
      </div>
    </div>
  )
}
