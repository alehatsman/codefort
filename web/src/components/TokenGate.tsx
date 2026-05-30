import { useState } from "react"
import { setToken } from "../api/client"

interface Props {
  onSet: () => void
}

export default function TokenGate({ onSet }: Props) {
  const [value, setValue] = useState("")
  const [error, setError] = useState<string | null>(null)

  function submit(e: React.FormEvent) {
    e.preventDefault()
    const trimmed = value.trim()
    if (!trimmed.startsWith("mgt_")) {
      setError("Tokens start with mgt_")
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
          />
          {error && <div className="error">{error}</div>}
          <button type="submit">Continue</button>
        </form>
      </div>
    </div>
  )
}
