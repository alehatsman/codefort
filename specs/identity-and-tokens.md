---
id: identity-and-tokens
status: draft
owners: [aleh]
covers:
  - "internal/server/auth.go"
  - "internal/server/tokens.go"
  - "internal/storage/tokens.go"
---
# Identity & Tokens

## Intent

moongit's whole point is to coordinate a fleet of agents (and humans) that act
under distinct, attributable identities. A bearer token *is* an identity: its
name is stamped as the author/assignee/trigger on every write, so the issue
tracker, claims, PRs, and the event feed can attribute and serialize work. The
security posture is deliberately **local-trust**: the daemon runs on a trusted
box for a known fleet, so the bar is "a valid token" — authentication, for
attribution and a network gate — not per-resource authorization. The data plane
is intentionally open; the social locks (a claim, author-only deletes) do the
ownership work, not a permission model. This spec owns the auth posture the
git-hosting, issues, and pull-requests specs defer to it.

## Behavior

- WHEN a token is minted, the server returns its plaintext (`mgt_` + 64 hex)
  exactly once and stores only its SHA-256 hash, so a missed response is
  unrecoverable and the secret never lives in the database.
- WHERE a token name is blank, over 100 chars, or already taken, minting is
  rejected; the name is the token's identity, so it is unique.
- WHEN any `/api` request arrives, it must carry a valid bearer token in the
  `Authorization` header; a missing, malformed, unknown, or revoked token is
  rejected as unauthorized.
- WHEN a token authenticates a write, the server stamps the canonical
  author/assignee/trigger from that token's name and ignores any client-supplied
  identity, so attribution can't be spoofed.
- WHEN a token is revoked, it stops authenticating immediately; an authentication
  lookup cannot distinguish unknown from revoked.
- WHILE a token is used repeatedly, its last-used timestamp is updated at most
  once per debounce window, so a polling fleet doesn't amplify reads into a
  write per request.
- WHEN a caller asks who it is, the server returns the authenticated token's
  name; listing tokens returns every token's metadata (active and revoked) to any
  authenticated caller — tokens are not owned by a user, and the plaintext is
  never recoverable.
- WHILE a single Basic credential is configured, the git smart-HTTP surface and
  the web UI require it; unset, they stay open. The `/api` surface always uses
  its own bearer auth regardless, and `/healthz` is always open.
- WHILE a valid token is present, the data plane is open: any token may mint or
  revoke tokens, create/update/delete issues and PRs, and claim work. Ordering
  and ownership come from the claim lock and author-only deletes, not from
  per-token permissions.
- WHERE a run needs to act as itself, an ephemeral per-run token is minted with a
  deterministic name and revoked when the run finalizes, so automated actors get
  the same attributable identity without a long-lived secret.

## Non-goals

- **SSH public-key identity.** Resolving an SSH public key to its owning token
  identity is the ssh-transport spec; here identity means a bearer token.
- **The claim mechanism.** How a claim locks work is the issues spec; this spec
  only states that the claim — not a permission model — is what serializes it.
- **Agent credential injection.** How a run's token (and LLM/dex creds) are wired
  into a container is the agent-runs spec; here only the mint/revoke contract.
- **Roles, scopes, per-resource authorization, multi-user accounts.** Local-trust
  is the design: tokens are identities, not principals with permissions, and
  there is deliberately no RBAC, no per-repo ACL, and no scoped/expiring token.
- **Transport encryption.** TLS/termination is an operator/deployment concern,
  not specified here.

## Checklist

- [x] Mint returns plaintext once; only the SHA-256 hash is stored
- [x] `mgt_`-prefixed tokens; unique, length-bounded names
- [x] Bearer required on all `/api`; unknown/revoked/missing → unauthorized
- [x] Identity stamped server-side from the token; client identity ignored
- [x] Revocation takes effect immediately; debounced last-used updates
- [x] whoami + full token list (active + revoked) to any authed caller
- [x] Optional Basic gates git + web; `/api` bearer + open `/healthz` regardless
- [x] Open data plane; claims + author-only deletes are the social locks
- [x] Ephemeral per-run token, deterministic name, revoked on finalize
- [ ] Verified against the code by the verify workflow (flip to `living`)
