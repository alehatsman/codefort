import clsx from "clsx"
import "./settings.css"
import { useEffect, useState } from "react"
import { useAgentSettings, useSSHKeys, useTokens, useWhoami } from "@/api/queries"
import {
  useAddSSHKey,
  useCreateToken,
  useDeleteSSHKey,
  useRevokeToken,
  useUpdateAgentSettings,
} from "@/api/mutations"
import type { CIRunExecutionModel, CreatedToken, SSHKey, Token } from "@/api/types"
import ThemeSelect from "@/features/settings/ThemeSelect"
import { Badge, Button, EmptyState, ErrorMessage, Input, Select, Spinner } from "@/ui"

// Sections of the settings surface. "tokens", "agent", "ssh", and "appearance"
// are backed. "users" and "branches" are planned features whose server backends
// aren't built yet, so they render as roadmap placeholders (not dead ends).
type Section = "tokens" | "agent" | "users" | "ssh" | "branches" | "appearance"

const SECTIONS: { id: Section; label: string }[] = [
  { id: "tokens", label: "API tokens" },
  { id: "agent", label: "Agent" },
  { id: "users", label: "Users" },
  { id: "ssh", label: "SSH keys" },
  { id: "branches", label: "Branch rules" },
  { id: "appearance", label: "Appearance" },
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
            note="Per-user accounts and management are planned. Identity is the API
              token's name today; this section fills in once that backend lands."
          />
        )}
        {section === "ssh" && <SSHKeysSection />}
        {section === "branches" && (
          <Placeholder
            title="Branch rules"
            note="Branch protection is planned. Rules take effect once the server
              enforces them on the push path; until then any token can push any branch."
          />
        )}
        {section === "appearance" && <AppearanceSection />}
      </div>
    </div>
  )
}

// AppearanceSection hosts the color-scheme picker. The choice is browser-local
// (localStorage), so there's nothing to save server-side.
function AppearanceSection() {
  return (
    <section className="settings__section">
      <h2 className="settings__title">Appearance</h2>
      <p className="muted settings__lead">
        Color scheme for the UI chrome and code view. Saved to this browser.
      </p>
      <ThemeSelect />
    </section>
  )
}

// A section for a feature that's on the roadmap but whose backend isn't built
// yet. The "Planned." lead signals an intentional, coming feature — not an
// abandoned stub or a hard non-goal.
function Placeholder({ title, note }: { title: string; note: string }) {
  return (
    <section className="settings__section">
      <h2 className="settings__title">{title}</h2>
      <EmptyState>
        <strong>Planned.</strong> {note}
      </EmptyState>
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
  const [authToken, setAuthToken] = useState("")
  const [baseUrl, setBaseUrl] = useState("")
  const configured = settingsQ.data?.claude_oauth_token_set ?? false
  const envFallback = settingsQ.data?.claude_token_env_fallback ?? false
  const authConfigured = settingsQ.data?.anthropic_auth_token_set ?? false
  const savedBaseUrl = settingsQ.data?.llm_base_url ?? ""

  // The base URL is shown (not a secret), so seed the editable field from the
  // server value — including after a save invalidates and refetches it.
  useEffect(() => setBaseUrl(savedBaseUrl), [savedBaseUrl])

  function save(e: React.FormEvent) {
    e.preventDefault()
    const t = token.trim()
    if (!t) return
    update.mutate({ claude_oauth_token: t }, { onSuccess: () => setToken("") })
  }

  function clear() {
    update.mutate({ claude_oauth_token: "" })
  }

  function saveBaseUrl(e: React.FormEvent) {
    e.preventDefault()
    update.mutate({ llm_base_url: baseUrl.trim() })
  }

  function saveAuthToken(e: React.FormEvent) {
    e.preventDefault()
    const t = authToken.trim()
    if (!t) return
    update.mutate({ anthropic_auth_token: t }, { onSuccess: () => setAuthToken("") })
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

      <div className={clsx("agent-token-status", { "is-set": configured || envFallback })}>
        {settingsQ.isLoading
          ? "Checking…"
          : configured
            ? "✓ A Claude token is configured here."
            : envFallback
              ? "✓ Authenticating via the server's environment fallback. Set a token here to override it."
              : "No Claude token configured — agent runs can't authenticate yet."}
      </div>

      <form className="agent-token-form" onSubmit={save}>
        <Input
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
          <Button
            type="submit"
            variant="primary"
            size="small"
            disabled={update.isPending || token.trim() === ""}
          >
            {update.isPending ? "Saving…" : "Save token"}
          </Button>
          {configured && (
            <Button variant="danger" size="small" onClick={clear} disabled={update.isPending}>
              Clear
            </Button>
          )}
        </div>
      </form>

      <div className="agent-default-model">
        <label className="agent-default-model__label">
          <span>Default execution model</span>
          <Select
            value={settingsQ.data?.execution_model ?? ""}
            disabled={update.isPending || settingsQ.isLoading}
            onChange={(e) =>
              update.mutate({ execution_model: e.target.value as "" | CIRunExecutionModel })
            }
          >
            <option value="">Server default (claude-edit)</option>
            <option value="claude-edit">Claude (edit files)</option>
            <option value="mooncake-agent">Mooncake agent (run actions)</option>
          </Select>
        </label>
        <p className="muted small">
          The model new agent runs use when “Spawn agent” doesn’t pick one. Per-run choices at spawn
          still win.
        </p>
      </div>

      <div className="agent-endpoint">
        <h3 className="settings__subtitle">Custom endpoint</h3>
        <p className="muted small">
          Point agent runs at an Anthropic-compatible gateway or local model. The base URL is
          injected as <code>ANTHROPIC_BASE_URL</code> (overriding{" "}
          <code>MOONGIT_AGENT_LLM_BASE_URL</code>
          ); the auth token as <code>ANTHROPIC_AUTH_TOKEN</code>, which then becomes the agent's
          auth (replacing the Claude token above). The token is stored write-only — it's never shown
          again. Leave both blank to use Anthropic directly.
        </p>

        <form className="agent-token-form" onSubmit={saveBaseUrl}>
          <Input
            type="url"
            value={baseUrl}
            onChange={(e) => setBaseUrl(e.target.value)}
            placeholder="https://gateway.example.com"
            aria-label="LLM base URL"
            autoComplete="off"
          />
          <div className="agent-token-form__actions">
            <Button
              type="submit"
              variant="primary"
              size="small"
              disabled={update.isPending || baseUrl.trim() === savedBaseUrl}
            >
              {update.isPending ? "Saving…" : "Save base URL"}
            </Button>
            {savedBaseUrl !== "" && (
              <Button
                variant="danger"
                size="small"
                onClick={() => update.mutate({ llm_base_url: "" })}
                disabled={update.isPending}
              >
                Clear
              </Button>
            )}
          </div>
        </form>

        <div className={clsx("agent-token-status", { "is-set": authConfigured })}>
          {settingsQ.isLoading
            ? "Checking…"
            : authConfigured
              ? "✓ A gateway auth token is configured."
              : "No gateway auth token — runs authenticate with the Claude token above."}
        </div>

        <form className="agent-token-form" onSubmit={saveAuthToken}>
          <Input
            type="password"
            value={authToken}
            onChange={(e) => setAuthToken(e.target.value)}
            placeholder={authConfigured ? "Replace auth token" : "Paste auth token"}
            aria-label="Gateway auth token"
            autoComplete="off"
          />
          <div className="agent-token-form__actions">
            <Button
              type="submit"
              variant="primary"
              size="small"
              disabled={update.isPending || authToken.trim() === ""}
            >
              {update.isPending ? "Saving…" : "Save auth token"}
            </Button>
            {authConfigured && (
              <Button
                variant="danger"
                size="small"
                onClick={() => update.mutate({ anthropic_auth_token: "" })}
                disabled={update.isPending}
              >
                Clear
              </Button>
            )}
          </div>
        </form>
      </div>

      <ErrorMessage error={update.error} inline />
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
        <Input
          placeholder="token name (e.g. ci-bot)"
          value={name}
          onChange={(e) => setName(e.target.value)}
          maxLength={100}
        />
        <Button variant="primary" type="submit" disabled={!trimmed || create.isPending}>
          {create.isPending ? "Creating…" : "Create token"}
        </Button>
      </form>
      <ErrorMessage error={create.error} inline />

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
            <Button size="small" onClick={copySecret}>
              {copied ? "Copied" : "Copy"}
            </Button>
          </div>
        </div>
      )}

      {tokensQ.isLoading && <Spinner />}
      <ErrorMessage error={tokensQ.error} />
      <ErrorMessage error={revoke.error} inline />

      {tokensQ.data && tokensQ.data.length === 0 && (
        <EmptyState>No tokens yet. Create one above.</EmptyState>
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
                  {t.name === whoami.data?.name && <Badge>you</Badge>}
                </td>
                <td className="muted">{new Date(t.created_at).toLocaleDateString()}</td>
                <td className="muted">
                  {t.last_used_at ? new Date(t.last_used_at).toLocaleDateString() : "never"}
                </td>
                <td>
                  {t.revoked_at ? (
                    <Badge state="closed">revoked</Badge>
                  ) : (
                    <Badge state="done">active</Badge>
                  )}
                </td>
                <td className="token-table__actions">
                  {!t.revoked_at && (
                    <Button
                      variant="danger"
                      size="small"
                      onClick={() => onRevoke(t)}
                      disabled={revoke.isPending}
                    >
                      Revoke
                    </Button>
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
        <Input
          placeholder="label (optional — defaults to the key's comment)"
          value={comment}
          onChange={(e) => setComment(e.target.value)}
          maxLength={100}
        />
        <Button variant="primary" type="submit" disabled={!trimmed || add.isPending}>
          {add.isPending ? "Adding…" : "Add SSH key"}
        </Button>
      </form>
      <ErrorMessage error={add.error} inline />

      {keysQ.isLoading && <Spinner />}
      <ErrorMessage error={keysQ.error} />
      <ErrorMessage error={del.error} inline />

      {keysQ.data && keysQ.data.length === 0 && (
        <EmptyState>No SSH keys yet. Add one above.</EmptyState>
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
                  <Button
                    variant="danger"
                    size="small"
                    onClick={() => onDelete(k)}
                    disabled={del.isPending}
                  >
                    Remove
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  )
}
