---
id: specs
status: draft
owners: [aleh]
covers:
  - "internal/specs/**"
  - "internal/server/specs.go"
  - "internal/storage/verifications.go"
---
# Specs

## Intent

Specs are human-authored, high-altitude descriptions of what the system *should*
do, stored in the repo under `specs/` and versioned alongside the code they
govern. They are the dual of the dex-derived Explore view, which describes what
the code *is* — and the payoff is continuously diffing the two, so a spec that
no longer matches its code surfaces as drift. This spec describes the Specs
*feature*: how specs are read, written, searched, and checked for drift. The
markdown format itself — frontmatter fields, the Intent/Behavior/Non-goals/
Checklist convention, EARS style — is the contract in `docs/specs.md`, which
this spec references rather than restates. (This file is itself a spec, and is
verified by the very workflow it describes.)

## Behavior

- WHEN a client lists specs on a ref, the server returns each markdown spec under
  `specs/` with its parsed metadata; a repo with no `specs/` directory (or no
  commits) returns an empty list, not an error.
- WHEN a client fetches one spec, the server returns its raw content plus the
  parsed structure — frontmatter, sections, and checklist items with
  file-absolute line numbers — and a spec whose frontmatter is malformed is still
  returned best-effort so it can be fixed.
- WHEN a client writes a spec, the server commits the content to a feature branch
  in the bare repo (worktree-free) and returns the branch and commit, never
  writing the default branch directly — so a spec changes through the same pull
  request flow as code.
- WHEN a client searches specs, the server runs a dex semantic search scoped to
  the `specs/` corpus, so a spec is findable by meaning, not just by path.
- WHEN a client requests drift, the server classifies each spec deterministically
  against the code it governs: `uncovered` (no `covers` globs, so drift can't be
  checked), `unverified` (covered but never verified), `stale` (a governed glob
  changed since the spec was last verified), or `fresh` (no change) — and a
  failed diff errs toward `stale`, never falsely `fresh`.
- WHERE drift classification runs, it is a non-LLM backstop: the `unverified` and
  `stale` specs are the candidate set a verify pass should run over, while
  `fresh` specs are skipped for cost.
- WHERE a spec is verified, the result is a *match* report — a `[0,1]` alignment
  (the fraction of verifiable lines the code bears out) plus per-line markers
  (aligned / drifted / unverifiable / unspecced) keyed to file-absolute lines for
  a truth gutter — and explicitly not a correctness or quality claim: a fully
  aligned spec can still be the wrong spec.
- WHEN a verify pass completes for a spec, it should stamp `last_verified` and
  `alignment` onto the spec's frontmatter and record the per-line verdicts, so
  the drift backstop and the UI reflect the latest agreement.
- WHILE a spec's status is `draft`, it is in progress and not yet authoritative;
  once authoritative it is `living` (the verify workflow maintains its stamps
  from there); a replaced spec becomes `superseded` rather than being deleted.

## Non-goals

- **The spec file format.** Frontmatter fields, the named-section convention, and
  EARS-style behavior are the `docs/specs.md` contract; this spec governs the
  feature, not the grammar.
- **Spec quality scoring.** Whether a spec is *good* (validity ≠ quality) is a
  separate judgment (#231); verification only measures spec↔code agreement.
- **Reconcile actions.** Turning drift into proposed issues, backfills, or
  supersessions is its own workflow (the #223–#226 line), not this read/write/
  verify surface.
- **The web Specs tab.** The tree, status rail, truth gutter, editor, and command
  palette are web-side rendering of this data, specified elsewhere.
- **dex and the verify agent run.** The semantic index is the code-intel/dex
  domain; running the LLM verify pass reuses the agent-runs spine. This spec owns
  the spec data and its deterministic drift, not those engines.

## Checklist

- [x] List specs on a ref; empty list (not 404) when none
- [x] Get a spec: raw + parsed structure; malformed frontmatter best-effort
- [x] Write a spec to a feature branch (worktree-free); changes land via PR
- [x] Semantic search scoped to the `specs/` corpus
- [x] Deterministic drift: uncovered / unverified / stale / fresh; fail → stale
- [x] Verification result model: alignment + per-line markers (match, not quality)
- [x] Lenient parser: renders on bad metadata; Validate() reports semantic issues
- [ ] Verify pass stamps last_verified/alignment + records per-line verdicts (#219, #220)
- [ ] Verified against the code by the verify workflow (flip to `living`)
