import clsx from "clsx"
import { useState } from "react"
import { useAgentSettings, useTokens, useWhoami } from "../api/queries"
import { useCreateToken, useRevokeToken, useUpdateAgentSettings } from "../api/mutations"
import type { CreatedToken, Token } from "../api/types"

// Sections of the settings surface. "tokens" and "agent" are backed; the
// rest are placeholders until their backends exist (no per-user mgmt, no
// SSH git transport, no branch-protection enforcement yet).
type Section = "tokens" | "agent" | "users" | "ssh" | "branches"

const SECTIONS: { id: Section; label: string }[] = [
  { id: "tokens", label: "API tokens" },
  { id: "agent", label: "Agent" },
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
        {section === "agent" && <AgentSection />}
        {section === "users" && (
          <Placeholder
            title="Users"
            note="Identity comes from API token names today; there's no per-user
              account model yet. User management lands when that backend exists."
          />
        )}
        {section === "ssh" && (
          <Placeholder
            title="SSH keys"
            note="Git is served over smart-HTTP with Bearer tokens; there's no SSH
              transport yet, so there's nothing for SSH keys to authenticate."
          />
        )}
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

// AgentSection sets the global Claude token agent runs authenticate with. The
// token is write-only: the API reports only whether one is configured, so the
// field is always blank and submitting replaces it.
function AgentSection() {
  const settingsQ = useAgentSettings()
  const update = useUpdateAgentSettings()
  const [token, setToken] = useState("")
  const configured = settingsQ.data?.claude_oauth_token_set ?? false

  function save(e: React.FormEvent) {
    e.preventDefault()
    const t = token.trim()
    if (!t) return
    update.mutate({ claude_oauth_token: t }, { onSuccess: () => setToken("") })
  }

  function clear() {
    update.mutate({ claude_oauth_token: "" })
  }

  return (
    <section className="settings__section">
      <h2 className="settings__title">Agent</h2>
      <p className="muted small">
        The Claude token agent runs authenticate with (a value from <code>claude setup-token</code>
        ), injected into each agent container as <code>CLAUDE_CODE_OAUTH_TOKEN</code>. It overrides
        the <code>MOONGIT_AGENT_CLAUDE_OAUTH_TOKEN</code> env, so you can set it here without
        restarting the server. Stored write-only — it's never shown again.
      </p>

      <div className={clsx("agent-token-status", { "is-set": configured })}>
        {settingsQ.isLoading
          ? "Checking…"
          : configured
            ? "✓ A Claude token is configured."
            : "No Claude token configured — agent runs can't authenticate yet."}
      </div>

      <form className="agent-token-form" onSubmit={save}>
        <input
          className="input"
          type="password"
          value={token}
          onChange={(e) => setToken(e.target.value)}
          placeholder={
            configured ? "Replace token (sk-ant-oat01-…)" : "Paste token (sk-ant-oat01-…)"
          }
          aria-label="Claude token"
          autoComplete="off"
        />
        <div className="agent-token-form__actions">
          <button
            type="submit"
            className="btn btn--small btn--primary"
            disabled={update.isPending || token.trim() === ""}
          >
            {update.isPending ? "Saving…" : "Save token"}
          </button>
          {configured && (
            <button
              type="button"
              className="btn btn--small btn--danger"
              onClick={clear}
              disabled={update.isPending}
            >
              Clear
            </button>
          )}
        </div>
      </form>
      {update.error && <div className="error inline">{(update.error as Error).message}</div>}
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
