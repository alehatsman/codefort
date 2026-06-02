import { useState } from "react"
import {
  Badge,
  Button,
  Card,
  EmptyState,
  FilterChip,
  Input,
  Select,
  Spinner,
  Textarea,
} from "../components/ui"
import type { BadgeState, ButtonVariant } from "../components/ui"

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
          The shared base components in <code>components/ui</code>. Use the theme switcher in the
          top bar to check every variant against each color scheme.
        </p>
      </header>

      <ButtonsSection />
      <BadgesSection />
      <ChipsSection />
      <CardsSection />
      <FeedbackSection />
      <FormSection />
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

const BADGE_STATES: BadgeState[] = ["todo", "in_progress", "done", "closed"]

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
    </>
  )
}

function FormSection() {
  const [text, setText] = useState("")
  const [body, setBody] = useState("")
  const [choice, setChoice] = useState("one")
  return (
    <Section title="Form inputs">
      <Input
        placeholder="Input…"
        value={text}
        onChange={(e) => setText(e.target.value)}
        aria-label="Demo input"
      />
      <Select value={choice} onChange={(e) => setChoice(e.target.value)} aria-label="Demo select">
        <option value="one">Option one</option>
        <option value="two">Option two</option>
      </Select>
      <Textarea
        placeholder="Textarea…"
        rows={3}
        value={body}
        onChange={(e) => setBody(e.target.value)}
        aria-label="Demo textarea"
      />
    </Section>
  )
}
