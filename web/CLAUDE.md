# codefort web — conventions

Vite + React 19 SPA. Read this before adding or editing components.

How the UI is split into pieces and styled is the fleet's
[ts-quality docs/UI.md](https://github.com/alehatsman/ts-quality/blob/main/docs/UI.md):
BEM strictly, shared blocks in one base stylesheet, promote a primitive on the second
consumer, domain→presentation mapping stays in the feature, tokens on `:root`,
motion and a11y rules, no utility framework / CSS-in-JS / CSS modules. This file is
only the codefort delta: where things live here, the primitives, the tooling.

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
blocks in `styles.css`, exported from the `@/ui` barrel. Build new pages by
composing these, not by hand-stitching `className` strings.

- **Promotion path (UI.md rule 11) here:** the presentation shell moves into
  `@/ui`; the domain→presentation mapping stays in the feature (`StatusIcon` owns
  the glyph SVGs; `StateIcon`/`CIStatusIcon` map a domain status to a
  `{glyph, colorClass}` over it). Per-instance styling via a `className`/option prop.
- **Every primitive gets a `/dev/ui` row.** `DevGalleryPage.tsx` is the living
  gallery (our Storybook) and the design-token reference; add a section when you
  add a primitive or variant. The schemes are `github` and `monokai`
  (`src/theme.ts`), switched in Settings → Appearance. Light/dark is a separate
  axis: the base scheme follows `prefers-color-scheme` via a `@media` block in
  `styles.css`, so a primitive needs checking in both schemes *and* both system
  appearances.
- **Caller-derived state stays out of the primitive** (route matching, mutation
  wiring — see `Tab`'s `active` prop).

## Where a block lives here

- **Feature-specific blocks:** a co-located stylesheet, `features/<x>/<x>.css`
  (e.g. `features/issues/issues.css`, `features/pulls/pulls.css`) and
  `shell/shell.css`, imported by that feature's page component(s).
- **The shared base is `src/styles.css`:** theme vars + dark-mode `@media`, global
  resets, utilities (`.muted`, `.small`), the syntax-highlight (`hljs-*`) tokens,
  and the design-system primitives — `.btn`, `.card`,
  `.input`/`.select`/`.textarea`/`.field`, `.badge`, `.chip`, the app shell
  (`.app`/`.topbar`/`.main`/`.tabs`), and any block used across more than one
  feature (`comment`, `issue-row`, `markdown-body`, `diff-file`).
- Vite bundles all of it into one CSS file in prod; the split is for source
  locality, not runtime scoping. Class names are part of the contract — Playwright
  specs and the `is-*` / vim-nav selectors target them.
- **Conditional classes go through `clsx`** (UI.md rule 8): a `?`/`&&` in the
  className → `clsx`; a plain `${value}` interpolation stays a template literal.
- Modifier words are hyphenated (`--in-progress`), not the API's `in_progress`;
  map the enum at the call site. ui-lint flags the six underscored ones that
  exist today as warnings.

## Lint + format: Biome

`biome.jsonc` governs both (extending ts-quality's `biome.base.json`). Run before
committing:

- `npm run lint` — check (CI-equivalent)
- `npm run lint:fix` — check + autofix/format

Style: **no semicolons** (`semi: false`). Imports are auto-ordered by Biome
(don't hand-sort). a11y rules are error-level — fix the violation rather than
demote the rule; suppress a deliberate exception inline with a justified
`// biome-ignore lint/a11y/<rule>: <reason>`. The consumer file is `biome.jsonc`
on purpose: Biome reads a `biome.json` as strict JSON and, on a parse error, runs
with defaults instead of failing. Never demote a rule there without a comment
saying why; config-check reports every rule switched off.

The gate's ui-lint step (`provision apply tasks/ui-ci.yml`) reports BEM and
raw color/radius/duration literals in stylesheets as warnings.

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
