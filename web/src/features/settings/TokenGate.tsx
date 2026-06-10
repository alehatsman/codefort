import { useState } from "react"
import "./settings.css"
import { setToken, verifyToken } from "@/api/client"
import { useLogin, useRegister } from "@/api/mutations"
import { ErrorMessage } from "@/ui"

interface Props {
  onSet: () => void
}

type Tab = "token" | "login" | "register"

export default function TokenGate({ onSet }: Props) {
  const [tab, setTab] = useState<Tab>("token")

  return (
    <div className="gate">
      <div className="gate__card">
        <h1>moongit</h1>

        <div className="gate__tabs">
          <button
            type="button"
            className={`gate__tab${tab === "token" ? " is-active" : ""}`}
            onClick={() => setTab("token")}
          >
            Use token
          </button>
          <button
            type="button"
            className={`gate__tab${tab === "login" ? " is-active" : ""}`}
            onClick={() => setTab("login")}
          >
            Sign in
          </button>
          <button
            type="button"
            className={`gate__tab${tab === "register" ? " is-active" : ""}`}
            onClick={() => setTab("register")}
          >
            Create account
          </button>
        </div>

        {tab === "token" && <TokenForm onSet={onSet} />}
        {tab === "login" && <LoginForm onSet={onSet} />}
        {tab === "register" && <RegisterForm onSet={onSet} />}
      </div>
    </div>
  )
}

function TokenForm({ onSet }: { onSet: () => void }) {
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
    <>
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
    </>
  )
}

function LoginForm({ onSet }: { onSet: () => void }) {
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")
  const login = useLogin()

  function submit(e: React.FormEvent) {
    e.preventDefault()
    const u = username.trim()
    const p = password.trim()
    if (!u || !p || login.isPending) return
    login.mutate(
      { username: u, password: p },
      {
        onSuccess: (res) => {
          setToken(res.secret)
          onSet()
        },
      }
    )
  }

  return (
    <>
      <p className="muted">Sign in with your username and password.</p>
      <form onSubmit={submit}>
        <input
          type="text"
          placeholder="username"
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          disabled={login.isPending}
          autoComplete="username"
          // biome-ignore lint/a11y/noAutofocus: first field in a focused tab.
          autoFocus
        />
        <input
          type="password"
          placeholder="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          disabled={login.isPending}
          autoComplete="current-password"
        />
        <ErrorMessage error={login.error} />
        <button type="submit" disabled={login.isPending || !username.trim() || !password.trim()}>
          {login.isPending ? "Signing in…" : "Sign in"}
        </button>
      </form>
    </>
  )
}

function RegisterForm({ onSet }: { onSet: () => void }) {
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")
  const register = useRegister()

  function submit(e: React.FormEvent) {
    e.preventDefault()
    const u = username.trim()
    const p = password.trim()
    if (!u || !p || register.isPending) return
    register.mutate(
      { username: u, password: p },
      {
        onSuccess: (res) => {
          setToken(res.secret)
          onSet()
        },
      }
    )
  }

  return (
    <>
      <p className="muted">Create a new account. Your token will be minted automatically.</p>
      <form onSubmit={submit}>
        <input
          type="text"
          placeholder="choose a username"
          value={username}
          onChange={(e) => setUsername(e.target.value)}
          disabled={register.isPending}
          autoComplete="username"
          // biome-ignore lint/a11y/noAutofocus: first field in a focused tab.
          autoFocus
        />
        <input
          type="password"
          placeholder="choose a password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          disabled={register.isPending}
          autoComplete="new-password"
        />
        <ErrorMessage error={register.error} />
        <button type="submit" disabled={register.isPending || !username.trim() || !password.trim()}>
          {register.isPending ? "Creating…" : "Create account"}
        </button>
      </form>
    </>
  )
}
