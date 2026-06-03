import { useEffect, useRef, useState } from "react"
import {
  Badge,
  Button,
  Card,
  Dialog,
  EmptyState,
  ErrorMessage,
  Field,
  FilterChip,
  Inline,
  Input,
  PageHeader,
  RelativeTime,
  SegmentedControl,
  Select,
  Spinner,
  Stack,
  StatusIcon,
  Tab,
  Tabs,
  Textarea,
  Toolbar,
} from "@/ui"
import type { BadgeState, ButtonVariant, StatusGlyph } from "@/ui"

/**
 * Living gallery for the base UI primitives — an in-repo alternative to
 * Storybook. Renders every primitive in every variant so the design surface is
 * inspectable in the real app (real theme, real CSS, real Vite). Reachable at
 * /dev/ui. Add a row here whenever you add a primitive or a variant.
 */
export default function DevGalleryPage() {
  return (
    <div className="gallery">
      <header className="gallery__intro">
        <h2>UI primitives</h2>
        <p className="gallery__lede">
          The shared base components in <code>src/ui</code>. Use the theme switcher in the top bar
          to check every variant — and every design token below — against each color scheme.
        </p>
      </header>

      <TokensSection />
      <LayoutSection />
      <ButtonsSection />
      <TabsSection />
      <BadgesSection />
      <StatusIconSection />
      <SegmentedControlSection />
      <ChipsSection />
      <CardsSection />
      <FeedbackSection />
      <FormSection />
      <RelativeTimeSection />
      <DialogSection />
    </div>
  )
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="gallery__section">
      <h3 className="gallery__heading">{title}</h3>
      <div className="gallery__row">{children}</div>
    </section>
  )
}

// Token groups shown as swatches/samples. These are the design-token contract
// from styles.css; keep in sync when the token scales change.
const COLOR_TOKENS = [
  "--bg",
  "--bg-canvas",
  "--bg-elev",
  "--border",
  "--fg",
  "--fg-muted",
  "--accent",
  "--accent-muted",
]
const STATE_TOKENS = ["--state-todo", "--state-in_progress", "--state-done", "--state-closed"]
// Stable identity so useCssVars' effect dep doesn't change every render.
const SWATCH_TOKENS = COLOR_TOKENS.concat(STATE_TOKENS)
const SPACE_TOKENS = ["--space-1", "--space-2", "--space-3", "--space-4", "--space-5", "--space-6"]
const RADIUS_TOKENS = ["--radius-sm", "--radius", "--radius-lg", "--radius-pill"]
const TEXT_TOKENS = [
  "--text-xs",
  "--text-sm",
  "--text-base",
  "--text-md",
  "--text-lg",
  "--text-xl",
  "--text-2xl",
]

// Reads the live computed value of each CSS custom property, re-reading when
// the top-bar theme switcher flips <html data-theme>, so the resolved values
// below always match the active scheme.
function useCssVars(names: string[]): Record<string, string> {
  const [values, setValues] = useState<Record<string, string>>({})
  useEffect(() => {
    function read() {
      const cs = getComputedStyle(document.documentElement)
      setValues(Object.fromEntries(names.map((n) => [n, cs.getPropertyValue(n).trim()])))
    }
    read()
    const observer = new MutationObserver(read)
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["data-theme"],
    })
    return () => observer.disconnect()
  }, [names])
  return values
}

function TokensSection() {
  const values = useCssVars(SWATCH_TOKENS)
  return (
    <section className="gallery__section">
      <h3 className="gallery__heading">Design tokens — color</h3>
      <div className="gallery__tokens">
        {SWATCH_TOKENS.map((name) => (
          <div key={name} className="gallery__token">
            <div className="gallery__swatch" style={{ background: `var(${name})` }} />
            <span className="gallery__token-name">{name}</span>
            <span className="gallery__token-value">{values[name]}</span>
          </div>
        ))}
      </div>

      <h3 className="gallery__heading" style={{ marginTop: "var(--space-5)" }}>
        Design tokens — scales
      </h3>
      <div className="gallery__tokens">
        {SPACE_TOKENS.map((name) => (
          <div key={name} className="gallery__token">
            <div className="gallery__space" style={{ width: `var(${name})` }} />
            <span className="gallery__token-name">{name}</span>
          </div>
        ))}
        {RADIUS_TOKENS.map((name) => (
          <div key={name} className="gallery__token">
            <div className="gallery__radius" style={{ borderRadius: `var(${name})` }} />
            <span className="gallery__token-name">{name}</span>
          </div>
        ))}
        {TEXT_TOKENS.map((name) => (
          <div key={name} className="gallery__token">
            <span style={{ fontSize: `var(${name})` }}>Aa</span>
            <span className="gallery__token-name">{name}</span>
          </div>
        ))}
      </div>
    </section>
  )
}

const GAP_DEMO = [1, 2, 4, 6] as const

// Layout primitives are containers, so they're shown wrapping placeholder boxes
// rather than in the standard flex `.gallery__row`.
function LayoutSection() {
  return (
    <section className="gallery__section">
      <h3 className="gallery__heading">Layout — Stack / Inline / PageHeader / Toolbar</h3>

      <p className="gallery__sublabel">PageHeader — title (+ aside) and actions</p>
      <PageHeader title="Section title" actions={<Button variant="primary">New thing</Button>}>
        <Badge state="in_progress">2 open</Badge>
      </PageHeader>

      <p className="gallery__sublabel">Toolbar — filter/action bar (card surface)</p>
      <Toolbar label="Demo toolbar" card>
        <Button size="small">Filter</Button>
        <Button size="small">Sort</Button>
        <Badge>3 selected</Badge>
      </Toolbar>

      <p className="gallery__sublabel">Inline — horizontal, gap steps map to the spacing scale</p>
      <Stack gap={2}>
        {GAP_DEMO.map((g) => (
          <Inline key={g} gap={g}>
            <span className="gallery__muted" style={{ width: 48 }}>
              gap {g}
            </span>
            <span className="gallery__box" />
            <span className="gallery__box" />
            <span className="gallery__box" />
          </Inline>
        ))}
      </Stack>

      <p className="gallery__sublabel">Stack — vertical rhythm (gap 3)</p>
      <Stack gap={3}>
        <span className="gallery__box gallery__box--wide" />
        <span className="gallery__box gallery__box--wide" />
        <span className="gallery__box gallery__box--wide" />
      </Stack>
    </section>
  )
}

const VARIANTS: ButtonVariant[] = ["default", "primary", "ghost", "danger"]

function ButtonsSection() {
  return (
    <Section title="Button">
      {VARIANTS.map((variant) => (
        <Button key={variant} variant={variant}>
          {variant}
        </Button>
      ))}
      <Button variant="primary" size="small">
        small
      </Button>
      <Button disabled>disabled</Button>
    </Section>
  )
}

// All tabs point at /dev/ui so clicking a demo tab is a no-op; `active` is set
// by hand here since the gallery has no route to derive it from.
function TabsSection() {
  return (
    <Section title="Tab / Tabs">
      <Tabs label="Gallery demo tabs">
        <Tab to="/dev/ui" active>
          Active
        </Tab>
        <Tab to="/dev/ui">Default</Tab>
        <Tab to="/dev/ui" count={12}>
          With count
        </Tab>
        <Tab to="/dev/ui" disabled>
          Disabled
        </Tab>
      </Tabs>
    </Section>
  )
}

const BADGE_STATES: BadgeState[] = ["todo", "in_progress", "done", "closed"]

const GLYPHS: StatusGlyph[] = [
  "dot-ring",
  "clock",
  "check",
  "x",
  "slash",
  "alert",
  "pause",
  "merge",
]

// Glyphs render in the current text color (currentColor) here; in the app the
// domain icons pass a `*-icon--<status>` className that themes them.
function StatusIconSection() {
  return (
    <Section title="StatusIcon">
      {GLYPHS.map((glyph) => (
        <span key={glyph} className="row" title={glyph}>
          <StatusIcon glyph={glyph} label={glyph} size={20} />
          <span className="gallery__muted">{glyph}</span>
        </span>
      ))}
    </Section>
  )
}

function BadgesSection() {
  return (
    <Section title="Badge">
      <Badge>neutral</Badge>
      {BADGE_STATES.map((state) => (
        <Badge key={state} state={state}>
          {state}
        </Badge>
      ))}
    </Section>
  )
}

function ChipsSection() {
  const [checked, setChecked] = useState<Record<string, boolean>>({ todo: true, done: false })
  return (
    <Section title="FilterChip">
      {["todo", "in_progress", "done"].map((s) => (
        <FilterChip
          key={s}
          checked={checked[s] ?? false}
          onChange={() => setChecked((c) => ({ ...c, [s]: !c[s] }))}
        >
          {s}
        </FilterChip>
      ))}
    </Section>
  )
}

function SegmentedControlSection() {
  const [layout, setLayout] = useState<"split" | "unified">("split")
  const [pick, setPick] = useState("one")
  return (
    <Section title="SegmentedControl">
      <SegmentedControl
        label="Row demo"
        orientation="row"
        value={layout}
        onChange={setLayout}
        options={[
          { value: "split", label: "Split" },
          { value: "unified", label: "Unified" },
        ]}
      />
      <SegmentedControl
        label="Column demo"
        value={pick}
        onChange={setPick}
        options={[
          { value: "one", label: "Option one" },
          { value: "two", label: "Option two" },
          { value: "three", label: "Option three" },
        ]}
      />
    </Section>
  )
}

function CardsSection() {
  return (
    <Section title="Card">
      <Card>
        <strong>Plain card</strong>
        <p className="gallery__muted">A surface container. Composes its own inner markup.</p>
      </Card>
      <Card selected>
        <strong>Selected card</strong>
        <p className="gallery__muted">The keyboard-nav highlight (is-vim-selected).</p>
      </Card>
    </Section>
  )
}

function FeedbackSection() {
  return (
    <>
      <Section title="Spinner">
        <Spinner />
        <Spinner label="Fetching commits…" />
      </Section>
      <Section title="EmptyState">
        <EmptyState>No issues match these filters.</EmptyState>
        <EmptyState bordered>Bordered: nothing to show yet.</EmptyState>
      </Section>
      <Section title="ErrorMessage">
        <ErrorMessage error={new Error("Something went wrong.")} />
        <ErrorMessage error={new Error("Inline variant — sits within a form body.")} inline />
      </Section>
    </>
  )
}

function FormSection() {
  const [text, setText] = useState("")
  const [body, setBody] = useState("")
  const [choice, setChoice] = useState("one")
  return (
    <Section title="Form inputs">
      <Field label="Text input">
        <Input placeholder="Input…" value={text} onChange={(e) => setText(e.target.value)} />
      </Field>
      <Field label="Select">
        <Select value={choice} onChange={(e) => setChoice(e.target.value)}>
          <option value="one">Option one</option>
          <option value="two">Option two</option>
        </Select>
      </Field>
      <Field label="Textarea">
        <Textarea
          placeholder="Textarea…"
          rows={3}
          value={body}
          onChange={(e) => setBody(e.target.value)}
        />
      </Field>
    </Section>
  )
}

function RelativeTimeSection() {
  return (
    <Section title="RelativeTime">
      <RelativeTime iso="2020-01-01T00:00:00Z" />
      <RelativeTime className="muted small" iso="2026-05-30T12:00:00Z" />
    </Section>
  )
}

function DialogSection() {
  const ref = useRef<HTMLDialogElement>(null)
  const close = () => ref.current?.close()
  return (
    <Section title="Dialog">
      <Button onClick={() => ref.current?.showModal()}>Open dialog</Button>
      <Dialog
        ref={ref}
        title="Example dialog"
        onClose={close}
        footer={
          <>
            <Button onClick={close}>Cancel</Button>
            <Button variant="primary" onClick={close}>
              Done
            </Button>
          </>
        }
      >
        <Field label="A field">
          <Input placeholder="Inside the dialog…" />
        </Field>
        <ErrorMessage error={new Error("Errors render inline in the body.")} inline />
      </Dialog>
    </Section>
  )
}
