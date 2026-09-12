---
id: ssh-transport
status: draft
owners: [aleh]
covers:
  - "internal/server/ssh.go"
  - "internal/server/ssh_keys.go"
  - "internal/storage/ssh_keys.go"
---
# SSH Transport

## Intent

Git over SSH is an opt-in, additive second transport for the same repositories
codefort already serves over HTTP. It exists for clients that prefer key-based
git access over a Basic-auth HTTP remote, and it resolves an SSH public key to
the same token identity the HTTP path uses — so a push over SSH is attributed
and triggers CI identically to a push over HTTP. It is strictly additive: the
HTTP transport stays open and authoritative; turning SSH on never changes the
HTTP behavior, and turning it off is the default.

## Behavior

- WHILE `CODEFORT_SSH_ADDR` is set, the daemon listens for git SSH on that
  address; unset, no SSH listener starts and codefort stays a single HTTP port.
- WHEN a client connects over SSH, authentication is public-key only — there is
  no password and no anonymous access; the key's fingerprint is looked up in the
  registered SSH keys and resolved to its owning token.
- IF the presented public key matches no registered key, the connection is
  rejected.
- WHEN an authenticated SSH connection runs a git service, the push/pull identity
  is the owning token's name — the same identity the Bearer (HTTP) path stamps —
  so attribution is uniform across transports.
- WHERE a session requests anything other than a single `git-upload-pack` or
  `git-receive-pack` exec (a shell, a pty, an arbitrary command), it is rejected;
  the SSH surface runs only the two git pack services.
- WHEN the git service runs, it operates on the same on-disk bare repo as the
  HTTP path (same owner/repo resolution and path-traversal defense), and
  `GIT_PROTOCOL` is passed through so protocol v2 works.
- WHEN a push arrives over SSH, the post-receive CI notification fires exactly as
  on an HTTP push, with the pusher set to the authenticated key's token name.
- WHEN the daemon first needs a host key, it generates and persists an ed25519
  key (private, 0600) so the server's SSH host identity is stable across
  restarts.
- WHILE the SSH listener runs, a transient accept error is retried after a short
  backoff rather than tearing down the transport; the listener closes only on
  shutdown.
- WHEN a client manages SSH keys over `/api/ssh-keys`, keys are scoped to the
  requesting token: it lists only its own keys, registers a public key against
  itself (whose token name becomes the SSH identity), and can delete only its own
  — another token's key id is indistinguishable from a missing one.
- IF a registered key is malformed it is rejected, and a duplicate key is a
  conflict.

## Non-goals

- **HTTP git transport.** Clone/fetch/push over smart-HTTP is the git-hosting
  spec; this spec is the SSH path and what is shared with it (bare repos, the CI
  hook).
- **Token identity & the data plane.** How tokens are minted and why the data
  plane is open is the identity-and-tokens spec; here a key just resolves to an
  existing token's name.
- **CI execution.** The post-receive hook firing is stated here as parity with
  HTTP; the run lifecycle it kicks off is the ci-pipelines spec.
- **SSH-level authorization.** Beyond "the key is registered," there is no
  per-repo or per-command SSH access control — matching the open data plane.
  Deliberate, not a gap.
- **SSH server hardening knobs.** No configurable ciphers/MACs/kex policy, no
  rate limiting, no CA-signed user certificates; the host key is a single
  self-managed ed25519 key.

## Checklist

- [x] Opt-in second listener via `CODEFORT_SSH_ADDR`; HTTP stays open + default
- [x] Public-key-only auth; fingerprint → owning token; unknown key rejected
- [x] SSH push/pull identity == Bearer-path token name (uniform attribution)
- [x] Only git-upload-pack / git-receive-pack exec; shell/pty/other rejected
- [x] Same bare repos + traversal defense; GIT_PROTOCOL v2 pass-through
- [x] Push fires the post-receive CI hook with the key's token name as pusher
- [x] Persisted ed25519 host key (0600), stable across restarts
- [x] Accept-error backoff/retry; clean shutdown on context cancel
- [x] Token-scoped SSH key list/add/delete; invalid→400, duplicate→409
- [ ] Verified against the code by the verify workflow (flip to `living`)
