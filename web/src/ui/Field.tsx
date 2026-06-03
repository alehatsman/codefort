import type { ReactNode } from "react"

interface Props {
  /** The `.field__label` caption shown above the control. */
  label: ReactNode
  /** The control — typically an Input/Textarea/Select primitive. */
  children: ReactNode
}

/**
 * A labelled form field — the `<label className="field">` + `.field__label`
 * pair repeated at ~10 call sites (NewIssueForm, NewRepoForm, EditIssueForm,
 * DraftReviewButton). The `<label>` wraps the control so the caption is its
 * accessible name with no `htmlFor` wiring.
 */
export default function Field({ label, children }: Props) {
  return (
    // biome-ignore lint/a11y/noLabelWithoutControl: the control is passed as children and rendered inside the label at runtime; the static check can't see through the children prop
    <label className="field">
      <span className="field__label">{label}</span>
      {children}
    </label>
  )
}
