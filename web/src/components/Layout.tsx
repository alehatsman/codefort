import { Link } from "react-router-dom"
import { clearToken } from "../api/client"

interface Props {
  children: React.ReactNode
  onSignOut: () => void
}

export default function Layout({ children, onSignOut }: Props) {
  function signOut() {
    clearToken()
    onSignOut()
  }

  return (
    <div className="app">
      <header className="topbar">
        <Link to="/" className="brand">
          moongit
        </Link>
        <button className="topbar__signout" onClick={signOut} title="Forget token">
          sign out
        </button>
      </header>
      <main className="main">{children}</main>
    </div>
  )
}
