---
id: access-control
status: draft
owners: [aleh]
covers:
  - "internal/server/access.go"
  - "internal/server/auth_accounts.go"
  - "internal/server/members.go"
  - "internal/storage/users.go"
  - "internal/storage/repo_members.go"
---
# Access Control — Accounts, Visibility, Membership

## Intent

codefort's base posture is local-trust: a valid token is the bar, and the data
plane is open (identity-and-tokens). That posture is right for a fleet whose
members already trust each other, and it stays the default — every repo is
`public`, and a default deployment behaves exactly as it did before this spec
existed.

But `VISION.md` explicitly leaves room on top of it for *lightweight*
refinements — named user accounts and coarse repo access — as conveniences for
a trusting team (avoiding mistakes, attributing work), not as a security
perimeter policing an adversary. This spec owns that layer: what an account is,
what `private` means, what membership grants, and — the part that matters most —
*where* the enforcement lives.

The enforcement question is not incidental. The first cut of this feature
shipped `CanAccessRepo` and `CanWriteRepo` with **zero call sites**: visibility
and membership were stored, returned by the API, and rendered in the UI, while
a repo marked private stayed readable through its direct URL and by anyone at
all over git smart-HTTP. A control that is advertised and not enforced is worse
than one that doesn't exist, because it is relied upon. So this spec fixes the
gate's *location* as part of the contract, not as an implementation detail.

The line this spec will not cross is the one `VISION.md` draws: coarse and
optional, never fine-grained and load-bearing. Owner / write / read is the whole
vocabulary. The *grid* of per-user, per-resource rules stays out.

## Behavior

- WHERE a repo is created, its owner is a `users` row (auto-created if absent),
  and its visibility defaults to `public` — so an existing deployment is
  unchanged by this feature until someone opts a repo in.
- WHEN a user registers with a username and password, the server stores a bcrypt
  hash and mints a bearer token named for the account; the registration endpoint
  is deliberately public, because on a local-trust box the network is the
  perimeter and there is no operator sitting by to approve sign-ups.
- WHERE a user is admin-provisioned rather than registered, the account carries
  no password hash and can authenticate only by its bearer token.
- WHEN a user logs in, a *fresh* token is minted and named `<user>-session`, so a
  browser session is revocable on its own without destroying the account's
  primary token.
- WHERE an access decision is made, the principal is the token's linked
  **account**, not the token's name — a session token named `alice-session`
  resolves to `alice`. A token with no linked account (admin-provisioned, or an
  ephemeral per-run agent token) falls back to matching on its name, which is
  how ownership resolved before accounts existed.
- WHILE a repo is `public`, every authenticated caller may read and write it,
  and the git transport serves it without a credential — the local-trust default,
  unchanged.
- WHILE a repo is `private`, read access requires the caller to be its owner or
  to hold a member row; write access additionally requires the member role to be
  `write`. The owner always has both.
- WHERE a private repo is denied to a caller, the server answers **404, not
  403** — a 403 confirms the repo exists, which is precisely what `private` is
  meant to hide. A caller who *can* read but not write gets a 403 instead, since
  the repo's existence is already no secret from them and naming the missing
  grant is the useful answer.
- WHERE the `/api` surface is served, access is enforced by a single middleware
  wrapping the whole route mux, inside the bearer-auth middleware and outside
  the router — so a newly added repo-scoped route is gated by construction and
  cannot forget to check. Requests that name no repo, and repos that do not
  resolve, pass through to the handler's own 404.
- WHERE the method is not a read (`GET`/`HEAD`/`OPTIONS`), write access is
  required; an unrecognized method is treated as a write, so the gate fails
  closed if a new verb is ever routed.
- WHERE git smart-HTTP is served, the same rules apply through a second
  middleware, which short-circuits on public repos so a public clone pays no
  auth cost and behaves exactly as before.
- WHERE a private repo is reached over git, the caller's identity comes from a
  cf token presented as a `Bearer` header or as the **password** half of
  HTTP Basic with any username — git has no bearer support, so this is the same
  token-over-git-HTTP shape as a forge personal access token. The shared
  `CODEFORT_BASIC_USER` credential does not satisfy this: it names a deployment,
  not a person, and so cannot resolve a principal.
- WHEN a private repo is requested over git with no usable credential, the
  server answers 401 with a `WWW-Authenticate: Basic` challenge, so `git clone`
  prompts for credentials rather than failing outright; a credential that is
  presented and rejected gets a 404 under the same no-leak rule.
- WHERE a push is attempted, both `git-receive-pack` and the `info/refs`
  advertisement that precedes it require write access, so a read-only
  collaborator is refused before uploading a pack rather than after.
- WHEN repos are listed, the result contains public repos plus the private ones
  the caller owns or belongs to — the list and the per-repo gate agree, so a
  repo never appears in one and 404s in the other.
- WHERE membership is granted or revoked, only the repo owner may do it, and the
  target must be an existing account — membership is an account relation, so a
  bare token name is not a member candidate.

## Non-goals

- **A security perimeter.** This is mistake-avoidance for a trusting team, not
  an adversarial control. The network, and optionally the shared Basic
  credential, remain the perimeter. A determined insider with any valid token is
  not the threat model.
- **A fine-grained RBAC matrix.** Owner / write / read is the entire vocabulary.
  Per-resource ACLs, custom roles, scopes, and expiring or scoped tokens stay
  out, per `VISION.md` and the constitution.
- **The token mechanism itself.** Minting, hashing, revocation, and the
  attribution stamp are identity-and-tokens; this spec consumes that contract
  and adds the principal/attribution distinction on top.
- **SSH transport access.** The SSH listener resolves a public key to an
  identity in ssh-transport; applying these rules there is not yet done (see the
  checklist).
- **Branch protection.** Restricting what a push may do to a ref is
  [branch-protection](branch-protection.md)'s job, enforced in a push hook
  rather than in this gate; this spec governs repo-level access only.
- **Transport encryption.** TLS termination is a deployment concern.

## Checklist

- [x] Repo owner is a users row; visibility defaults to `public`
- [x] Public self-registration with bcrypt; admin-provisioned users are token-only
- [x] Login mints a separately-revocable `<user>-session` token
- [x] Access resolves the token's account, falling back to the token name
- [x] Public repos: open read+write, open git transport — default unchanged
- [x] Private repos: owner or member to read; role `write` to write
- [x] No-read answers 404 (no existence leak); read-but-not-write answers 403
- [x] `/api` gated by one middleware over the whole mux, not per handler
- [x] Non-read methods require write; unknown methods fail closed
- [x] Git transport gated by the same rules, no-op for public repos
- [x] Token accepted as Basic password or Bearer on the git path
- [x] Anonymous private clone gets 401 + challenge; rejected credential gets 404
- [x] Push gated on both receive-pack and its ref advertisement
- [x] Repo list and per-repo gate agree on visibility
- [x] Owner-only membership changes against existing accounts
- [ ] Same rules applied to the git SSH transport
- [ ] Verified against the code by the verify workflow (flip to `living`)
