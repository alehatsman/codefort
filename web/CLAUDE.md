# moongit web — conventions

Vite + React 19 SPA. Read this before adding or editing components.

## Structure: organize by feature, import via `@/`

`src/` is grouped by **feature**, not by technical type:

- `src/features/<feature>/` — pages + components + helpers for one feature
  (`issues`, `pulls`, `repo`, `commits`, `pipelines`, `agents`, `explore`,
  `settings`). A feature owns everything specific to it; put new files for a
  feature here, not in a global drawer.
- `src/shell/` — cross-cutting app chrome (`Layout`, nav tabs, `Avatar`,
  `Markdown`, `NotFound`, `keyboardNav`, `timeAgo`).
- `src/ui/` — base UI primitives (Button/Card/Dialog/…) + the `/dev/ui` gallery.
- `src/api/` — the shared TanStack-Query surface (`client`, `queries`,
  `mutations`, `types`); one cross-feature layer, not split per feature.
- Root: `App.tsx`, `main.tsx`, `theme.ts`, `styles.css`.

Imports use the `@/` alias = `src/` (configured in `tsconfig.json` paths +
`vite.config.ts` resolve.alias) — e.g. `@/api/queries`, `@/shell/Layout`,
`@/ui`, `@/features/issues/CommentForm`. **No relative `../` imports** across
folders; keep them `@/`-absolute so files stay move-proof.

`pipelines/` owns the shared CI+agent run shell; `agents/` imports a couple of
helpers from it for now (a marked temporary seam — agents will grow its own).

## Styling: hand-written semantic BEM, no utility framework

- CSS lives in `src/styles.css` as semantic **BEM** — `block__element--modifier`
  (`board-col`, `board-col__head`, `board-col__head--done`). Theme values are
  CSS custom properties (`var(--border)`, `var(--fg-muted)`) defined alongside
  `src/theme.ts`. No Tailwind, no CSS-in-JS, no CSS modules.
- One class names the thing; modifiers (`--state`, `is-active`, `is-loading`,
  `is-vim-selected`) toggle variants. State flags use the `is-*` prefix.

## Composing className: use `clsx`, not template-literal ternaries

`clsx` is a dependency. Conditional classes go through it — never
`` `base ${cond ? "x" : ""}` `` (that leaves a trailing space / empty token).

```tsx
import clsx from "clsx"

// conditional modifier — object form
<div className={clsx("board-col", { "is-over": isOver })} />
<Link className={clsx("tab", { "is-active": isActive })} />

// optional passthrough className — clsx drops undefined cleanly
<div className={clsx("commit-meta", className)} />
```

Pure interpolation into a modifier (no conditional) stays a plain template
literal — `clsx` adds nothing there, so don't force it:

```tsx
<span className={`ci-badge ci-badge--${status}`} />        // fine
<td className={`diff-code diff-code--${kind}`} />          // fine
```

Rule of thumb: a `?`/`&&` in the className → `clsx`. Just `${value}` → template
literal.

## Lint + format: Biome

`biome.json` governs both. Run before committing:

- `npm run lint` — check (CI-equivalent)
- `npm run lint:fix` — check + autofix/format

Style: **no semicolons** (`semi: false`). Imports are auto-ordered by Biome
(don't hand-sort). a11y rules are error-level — fix the violation rather than
demote the rule; suppress a deliberate exception inline with a justified
`// biome-ignore lint/a11y/<rule>: <reason>`.

## Tests: Playwright

`npm test` runs the suite (`tests/*.spec.ts`). Add/extend a spec for new
interactive UI. The `vimnav` h/l tab-switch test can flake under parallel load
— re-run it isolated (`npx playwright test tests/vimnav.spec.ts:60`) before
treating a single failure as a regression.

## Worktree gotcha

A fresh git worktree needs its **own** `npm install` — don't symlink
`node_modules` from the primary checkout (two copies of `@playwright/test`
break the runner). After **rebasing** onto a moved `main`, run `npm install`
again: new devDeps may have landed (Biome did this way).
