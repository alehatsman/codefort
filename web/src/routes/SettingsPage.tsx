import clsx from "clsx"
import { useState } from "react"
import { useSSHKeys, useTokens, useWhoami } from "../api/queries"
import { useAddSSHKey, useCreateToken, useDeleteSSHKey, useRevokeToken } from "../api/mutations"
import type { CreatedToken, SSHKey, Token } from "../api/types"

// Sections of the settings surface. "tokens" and "ssh" are backed; "users"
// and "branches" stay placeholders until their backends exist (no per-user
// mgmt, no branch-protection enforcement yet).
type Section = "tokens" | "users" | "ssh" | "branches"

const SECTIONS: { id: Section; label: string }[] = [
  { id: "tokens", label: "API tokens" },
  { id: "users", label: "Users" },
  { id: "ssh", label: "SSH keys" },
  { id: "branches", label: "Branch rules" },
]

export default function SettingsPage() {
  const [section, setSection] = useState<Section>("tokens")

  return (
    <div className="settings">
      <nav className="settings__nav" aria-label="Settings sections">
        {SECTIONS.map((s) => (
          <button
            type="button"
            key={s.id}
            className={clsx("settings__nav-item", { "is-active": section === s.id })}
            onClick={() => setSection(s.id)}
          >
            {s.label}
          </button>
        ))}
      </nav>

      <div className="settings__content">
        {section === "tokens" && <TokensSection />}
        {section === "users" && (
          <Placeholder
            title="Users"
            note="Identity comes from API token names today; there's no per-user
              account model yet. User management lands when that backend exists."
          />
        )}
        {section === "ssh" && <SSHKeysSection />}
        {section === "branches" && (
          <Placeholder
            title="Branch rules"
            note="Branch protection needs enforcement in the push path, which
              isn't built yet. Rules will appear here once the server enforces them."
          />
        )}
      </div>
    </div>
  )
}

function Placeholder({ title, note }: { title: string; note: string }) {
  return (
    <section className="settings__section">
      <h2 className="settings__title">{title}</h2>
      <div className="empty">
        <strong>Coming soon.</strong> {note}
      </div>
    </section>
  )
}

function TokensSection() {
  const tokensQ = useTokens()
  const whoami = useWhoami()
  const create = useCreateToken()
  const revoke = useRevokeToken()

  const [name, setName] = useState("")
  // The plaintext is only ever returned once, at creation; hold it here so
  // the reveal banner survives re-renders until the user dismisses it.
  const [revealed, setRevealed] = useState<CreatedToken | null>(null)
  const [copied, setCopied] = useState(false)

  const trimmed = name.trim()

  function submit(e: React.FormEvent) {
    e.preventDefault()
    if (!trimmed || create.isPending) return
    create.mutate(
      { name: trimmed },
      {
        onSuccess: (tok) => {
          setRevealed(tok)
          setCopied(false)
          setName("")
        },
      }
    )
  }

  function onRevoke(t: Token) {
    if (t.revoked_at) return
    const self = t.name === whoami.data?.name ? " This is the token you're signed in with." : ""
    if (!window.confirm(`Revoke token "${t.name}"? It will stop working immediately.${self}`)) {
      return
    }
    revoke.mutate(t.id)
  }

  async function copySecret() {
    if (!revealed) return
    try {
      await navigator.clipboard.writeText(revealed.secret)
      setCopied(true)
    } catch {
      // Clipboard blocked (insecure context / permissions) — the secret is
      // still visible for manual copy, so just leave the button label.
    }
  }

  return (
    <section className="settings__section">
      <h2 className="settings__title">API tokens</h2>
      <p className="muted settings__lead">
        Tokens authenticate the API and git smart-HTTP. A token's name is its identity in every
        issue claim and comment. The secret is shown only once, at creation.
      </p>

      <form className="settings__create" onSubmit={submit}>
        <input
          className="input"
          placeholder="token name (e.g. ci-bot)"
          value={name}
          onChange={(e) => setName(e.target.value)}
          maxLength={100}
        />
        <button className="btn btn--primary" type="submit" disabled={!trimmed || create.isPending}>
          {create.isPending ? "Creating…" : "Create token"}
        </button>
      </form>
      {create.error && <div className="error inline">{(create.error as Error).message}</div>}

      {revealed && (
        <div className="token-reveal">
          <div className="token-reveal__head">
            <strong>New token “{revealed.name}”</strong>
            <button
              type="button"
              className="modal__close"
              onClick={() => setRevealed(null)}
              aria-label="Dismiss"
              title="Dismiss"
            >
              ×
            </button>
          </div>
          <p className="muted">
            Copy it now — it won't be shown again. Store it as <code>MOONGIT_TOKEN</code>.
          </p>
          <div className="token-reveal__secret">
            <code>{revealed.secret}</code>
            <button type="button" className="btn btn--small" onClick={copySecret}>
              {copied ? "Copied" : "Copy"}
            </button>
          </div>
        </div>
      )}

      {tokensQ.isLoading && <div className="loading">Loading…</div>}
      {tokensQ.error && <div className="error">{(tokensQ.error as Error).message}</div>}
      {revoke.error && <div className="error inline">{(revoke.error as Error).message}</div>}

      {tokensQ.data && tokensQ.data.length === 0 && (
        <div className="empty">No tokens yet. Create one above.</div>
      )}

      {tokensQ.data && tokensQ.data.length > 0 && (
        <table className="token-table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Created</th>
              <th>Last used</th>
              <th>Status</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {tokensQ.data.map((t) => (
              <tr key={t.id} className={t.revoked_at ? "is-revoked" : ""}>
                <td>
                  {t.name}
                  {t.name === whoami.data?.name && <span className="badge">you</span>}
                </td>
                <td className="muted">{new Date(t.created_at).toLocaleDateString()}</td>
                <td className="muted">
                  {t.last_used_at ? new Date(t.last_used_at).toLocaleDateString() : "never"}
                </td>
                <td>
                  {t.revoked_at ? (
                    <span className="badge badge--closed">revoked</span>
                  ) : (
                    <span className="badge badge--done">active</span>
                  )}
                </td>
                <td className="token-table__actions">
                  {!t.revoked_at && (
                    <button
                      type="button"
                      className="btn btn--small btn--danger"
                      onClick={() => onRevoke(t)}
                      disabled={revoke.isPending}
                    >
                      Revoke
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  )
}

function SSHKeysSection() {
  const keysQ = useSSHKeys()
  const add = useAddSSHKey()
  const del = useDeleteSSHKey()

  const [publicKey, setPublicKey] = useState("")
  const [comment, setComment] = useState("")

  const trimmed = publicKey.trim()

  function submit(e: React.FormEvent) {
    e.preventDefault()
    if (!trimmed || add.isPending) return
    add.mutate(
      { public_key: trimmed, comment: comment.trim() || undefined },
      {
        onSuccess: () => {
          setPublicKey("")
          setComment("")
        },
      }
    )
  }

  function onDelete(k: SSHKey) {
    const label = k.comment || k.fingerprint
    if (!window.confirm(`Remove SSH key "${label}"? Pushes signed by it will stop working.`)) {
      return
    }
    del.mutate(k.id)
  }

  return (
    <section className="settings__section">
      <h2 className="settings__title">SSH keys</h2>
      <p className="muted settings__lead">
        Public keys authenticate git over SSH (clone/push). A key inherits its token's identity, so
        a push lands as that token's name. Set <code>MOONGIT_SSH_ADDR</code> on the server to enable
        the transport.
      </p>

      <form className="settings__create settings__create--stacked" onSubmit={submit}>
        <textarea
          className="input"
          placeholder="ssh-ed25519 AAAA… your-comment"
          value={publicKey}
          onChange={(e) => setPublicKey(e.target.value)}
          rows={3}
        />
        <input
          className="input"
          placeholder="label (optional — defaults to the key's comment)"
          value={comment}
          onChange={(e) => setComment(e.target.value)}
          maxLength={100}
        />
        <button className="btn btn--primary" type="submit" disabled={!trimmed || add.isPending}>
          {add.isPending ? "Adding…" : "Add SSH key"}
        </button>
      </form>
      {add.error && <div className="error inline">{(add.error as Error).message}</div>}

      {keysQ.isLoading && <div className="loading">Loading…</div>}
      {keysQ.error && <div className="error">{(keysQ.error as Error).message}</div>}
      {del.error && <div className="error inline">{(del.error as Error).message}</div>}

      {keysQ.data && keysQ.data.length === 0 && (
        <div className="empty">No SSH keys yet. Add one above.</div>
      )}

      {keysQ.data && keysQ.data.length > 0 && (
        <table className="token-table">
          <thead>
            <tr>
              <th>Label</th>
              <th>Fingerprint</th>
              <th>Added</th>
              <th>Last used</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {keysQ.data.map((k) => (
              <tr key={k.id}>
                <td>{k.comment || <span className="muted">—</span>}</td>
                <td className="muted">
                  <code>{k.fingerprint}</code>
                </td>
                <td className="muted">{new Date(k.created_at).toLocaleDateString()}</td>
                <td className="muted">
                  {k.last_used_at ? new Date(k.last_used_at).toLocaleDateString() : "never"}
                </td>
                <td className="token-table__actions">
                  <button
                    type="button"
                    className="btn btn--small btn--danger"
                    onClick={() => onDelete(k)}
                    disabled={del.isPending}
                  >
                    Remove
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  )
}
