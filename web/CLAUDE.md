# codefort web — conventions

Vite + React 19 SPA. Read this before adding or editing components.

## Structure: organize by feature, import via `@/`

`src/` is grouped by **feature**, not by technical type:

- `src/features/<feature>/` — pages + components + helpers for one feature
  (`issues`, `pulls`, `repo`, `commits`, `pipelines`, `agents`, `specs`,
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

## Component library: `src/ui/` is the shared vocabulary

`src/ui/` is the in-repo component library — thin, typed wrappers over the BEM
blocks in `styles.css`, exported from the `@/ui` barrel. Treat it as the
default vocabulary: build new pages by composing these, not by hand-stitching
`className` strings.

- **When to add a primitive:** a pattern used by 2+ features (or one you're
  about to need for a new UI) graduates to `@/ui`. The presentation shell moves
  into the primitive; the **domain→presentation mapping stays in the feature**
  (e.g. `StatusIcon` owns the glyph SVGs; `StateIcon`/`CIStatusIcon` map a
  domain status to a `{glyph, colorClass}` over it). Keep primitives
  domain-agnostic — pass per-instance styling via a `className`/option prop.
- **Every primitive gets a `/dev/ui` row.** `DevGalleryPage.tsx` is the living
  gallery (our Storybook) and the design-token reference; add a section when you
  add a primitive or variant, and check it against both color schemes. The
  schemes are `github` and `monokai` (`src/theme.ts`), switched in
  Settings → Appearance — not the top bar. Light/dark is a separate axis: the
  base scheme follows `prefers-color-scheme` via a `@media` block in
  `styles.css`, so a primitive needs checking in both schemes *and* both system
  appearances.
- **Caller-derived state stays out of the primitive.** Route matching, mutation
  wiring, etc. live at the call site (see `Tab`'s `active` prop); the primitive
  owns markup + class composition only.

## Styling: hand-written semantic BEM, no utility framework

- Semantic **BEM** — `block__element--modifier` (`board-col`,
  `board-col__head`, `board-col__head--done`). Theme values are CSS custom
  properties (`var(--border)`, `var(--fg-muted)`) defined alongside
  `src/theme.ts`. No Tailwind, no CSS-in-JS, no CSS **modules** (the class
  names are part of the contract — Playwright specs and the `is-*` / vim-nav
  selectors target them; hashed names would break that).
- One class names the thing; modifiers (`--state`, `is-active`, `is-loading`,
  `is-vim-selected`) toggle variants. State flags use the `is-*` prefix.

### Where a block lives: co-located per feature

- **Feature-specific blocks** live in a co-located stylesheet next to the
  feature, imported by that feature's pages: `features/<x>/<x>.css` (e.g.
  `features/issues/issues.css`, `features/pulls/pulls.css`) and
  `shell/shell.css`. Add a feature's new block to its stylesheet — `import
  "./<x>.css"` from the feature's page component(s).
- **The shared base stays in `src/styles.css`**: theme vars + dark-mode
  `@media`, global resets, utilities (`.muted`, `.small`), the syntax-highlight
  (`hljs-*`) tokens, and the **design-system primitives** — `.btn`, `.card`,
  `.input`/`.select`/`.textarea`/`.field`, `.badge`, `.chip`, the app shell
  (`.app`/`.topbar`/`.main`/`.tabs`), and any block used directly across more
  than one feature (e.g. `comment`, `issue-row`, `markdown-body`, `diff-file`).
- Rule of thumb: used by one feature → that feature's stylesheet; used by the
  `ui/` primitives or across features → base `styles.css`. Vite bundles all of
  it into one CSS file in prod; the split is for source locality, not runtime
  scoping.

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
interactive UI. Every spec mocks the API through `tests/mockApi.ts` — no real
codefortd — which is what lets the suite boot a Vite dev server and stay
hermetic.

`tests/phase2-smoke.spec.ts` is the one exception: it hits a **real** codefortd
on `:8080` and so is excluded from the default config. Run it with
`npm run test:smoke` against a live server. Keep integration specs out of the
default net for the same reason — a suite that cannot pass locally stops being
read.

The `vimnav` h/l tab-switch test can flake under parallel load — re-run it
isolated (`npx playwright test tests/vimnav.spec.ts:60`) before treating a
single failure as a regression.

## Worktree gotcha

A fresh git worktree needs its **own** `npm install` — don't symlink
`node_modules` from the primary checkout (two copies of `@playwright/test`
break the runner). After **rebasing** onto a moved `main`, run `npm install`
again: new devDeps may have landed (Biome did this way).
