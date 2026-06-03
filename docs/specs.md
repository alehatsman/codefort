# Specs — in-repo specifications

A **spec** is a human-authored, high-altitude description of what a part of the
system *should* do. Specs live in the repo as markdown under `specs/`, version
alongside the code they govern, and are the dual of the dex-derived Explore view
(what the code *is*). The payoff is continuously diffing the two — drift
detection — but that builds on this convention.

This document is the contract. The shared parser is `internal/specs`
(`specs.Parse`); the list endpoint, the verify pre-pass, and spec quality
scoring all read specs through it, so anything documented here is what those
features can rely on.

## Where specs live

```
specs/
  constitution.md        # optional: repo-wide principles every spec inherits
  ssh-transport.md       # flat by default — one file per spec
  ci/                     # foldered only when a spec grows companions
    pipeline.md
    isolation.md
```

Keep it flat. Promote a spec to its own folder only when it accumulates
companion docs (diagrams, sub-specs) — not pre-emptively.

`specs/constitution.md`, if present, holds principles that apply repo-wide
(security posture, the single-binary rule, etc.). It is a spec like any other;
by convention other specs may reference it rather than restating it.

## File shape

A spec is plain markdown with an **optional** YAML frontmatter block:

```markdown
---
id: ssh-transport
status: living
owners: [aleh]
covers: ["internal/ssh/**", "cmd/moongitd/**"]
last_verified: 2026-06-02
alignment: 0.91
---
# SSH Transport

## Intent
...
```

Both the frontmatter block and every field in it are optional. A bare markdown
file with no frontmatter is a valid spec.

### Frontmatter fields

| field           | type            | meaning |
| --------------- | --------------- | ------- |
| `id`            | string          | Stable slug, independent of the path. Links and verification stamps key off it, so it survives file moves. **Defaults to the filename stem** (`ssh-transport.md` → `ssh-transport`). |
| `status`        | enum            | `living` (maintained, authoritative), `draft` (in progress, not yet trusted), or `superseded` (kept for history). Empty is treated as `living`. |
| `owners`        | list of strings | Who is accountable — token names or handles. |
| `covers`        | list of globs   | Code paths (repo-relative) the spec governs. The verify pre-pass diffs these globs against a push to decide whether the spec *might* have drifted. Empty means the spec claims no code surface and is never auto-flagged. |
| `last_verified` | date string     | `YYYY-MM-DD` the verify agent last confirmed the spec against the code. **Stamped by tooling — don't hand-edit.** |
| `alignment`     | number `0..1`   | The verify agent's last code↔spec agreement score. **Stamped by tooling.** Absent means never verified, which is distinct from `0.0`. |

Unknown frontmatter keys are ignored, so the format can grow without breaking
older parsers. A misspelled `status` or out-of-range `alignment` does **not**
stop a spec from rendering — the parser is lenient by design — but it is
reported by `specs.Spec.Validate`, which linters and the quality workflow run.

## Section convention

The body follows a loose, named-section convention. The parser extracts every
heading (so you can deep-link to a section), but these four names carry meaning
for the workflows:

- **`## Intent`** — *why* this exists and the outcome it guarantees. Prose.
- **`## Behavior`** — *what* it does, as testable statements. Write these
  [EARS-style](#ears-style-behavior): one observable behavior per line.
- **`## Checklist`** — GitHub task-list items (`- [ ]` / `- [x]`) the spec
  considers done-criteria. The parser collects these across the whole body.
- **`## Non-goals`** — what is deliberately out of scope. As load-bearing as the
  goals: it stops scope creep and tells the verify agent what *not* to flag.

A section subsumes its subsections — content under `## Behavior` up to the next
`##` (including any `###` beneath it) is that section's body.

### EARS-style behavior

EARS (Easy Approach to Requirements Syntax) keeps behavior statements testable
by giving each a trigger and a response:

```markdown
## Behavior
- WHEN a client pushes over SSH with a valid public key, the server resolves it
  to the key's owning token identity.
- WHILE the HTTP port stays open, SSH access is additive, not a replacement.
- IF the public key matches no `ssh_keys` row, the connection is rejected.
```

One behavior per bullet, each independently checkable. This is what makes
`covers` + verify meaningful.

## Write TIGHT specs

The failure mode of specs is bureaucracy. Guardrails:

- **Specify behavior, not implementation.** No pseudo-code, no function
  signatures, no "the parser shall use a stack." Those belong in the code; the
  spec says what the code must achieve.
- **High-altitude.** If a sentence only restates a line of code, cut it. A spec
  earns its keep by capturing intent the code can't express.
- **Non-goals are mandatory thinking, not filler.** Naming what you won't do is
  often more valuable than the goals.
- **Drift gates start non-blocking.** A spec being out of date is a signal, not
  a merge blocker — at least until the workflow has earned trust.

A good spec fits on a screen, survives a refactor untouched, and tells a new
contributor *why* before *how*.

## Lifecycle

1. **Draft** — write it (`status: draft`), often before or alongside the code.
2. **Living** — once it's authoritative, flip to `status: living`. The verify
   workflow stamps `last_verified` / `alignment` from here on.
3. **Superseded** — when a spec is replaced, set `status: superseded` and link
   to its successor rather than deleting it; the history is worth keeping.
