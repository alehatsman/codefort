// Review-issue templates (#159). The "Draft review issue" flow on the Review
// tab turns a chosen target into an issue title + body that drives a read-only
// review agent (toolProfile "review"). Keeping the four target variants in one
// module keeps their prompts consistent — the agent reads the body, reviews the
// target, and posts findings via the moongit `review_create` MCP tool (anchored
// to a file path + line range on a ref) with no shell and no file edits.

// ReviewTarget is the kind of thing a review agent is pointed at. Each maps to
// the existing code_comments anchor model (ref, path, line) differently.
export type ReviewTarget = "diff" | "commit" | "file" | "package"

export interface ReviewDraftInput {
  target: ReviewTarget
  // ref is the head ref / commit the agent reviews and anchors comments on:
  // the branch (diff/file/package) or the commit SHA (commit).
  ref: string
  // base is the diff's base ref (diff target only); the agent reviews head-vs-base.
  base?: string
  // path is the file (file target) or directory/package (package target) to review.
  path?: string
}

export interface ReviewDraft {
  title: string
  body: string
}

// The shared instructions every review issue carries: who the agent is, the
// one write channel it has (review_create), and the hard "read-only" boundary.
function preamble(anchorRef: string): string {
  return [
    "You are a **read-only review agent**.",
    "",
    "Post each finding as a code-review comment via the `review_create` MCP tool, anchored to the",
    `file path + line range on ref \`${anchorRef}\`. Keep each comment specific and actionable; cite`,
    "the exact lines. When you have reviewed everything, post a one-paragraph summary on this issue",
    "via `issue_comment`.",
    "",
    "Do **not** edit files, push branches, claim issues, or run pipelines — you only read and review.",
  ].join("\n")
}

// draftReviewIssue renders the issue title + body for a target. The caller
// validates that the target's required fields (path for file/package, base for
// diff) are present before calling.
export function draftReviewIssue(input: ReviewDraftInput): ReviewDraft {
  const ref = input.ref.trim()
  const base = (input.base ?? "").trim()
  const path = (input.path ?? "").trim()

  switch (input.target) {
    case "diff":
      return {
        title: `Review: ${base}..${ref}`,
        body: [
          `## Review the diff of \`${ref}\` against base \`${base}\``,
          "",
          "Review the changed lines in the head-vs-base diff. Focus on what the change introduces —",
          "correctness, edge cases, and anything that reads as a regression. Anchor comments on the",
          `head ref \`${ref}\`.`,
          "",
          preamble(ref),
        ].join("\n"),
      }
    case "commit":
      return {
        title: `Review: commit ${ref}`,
        body: [
          `## Review commit \`${ref}\``,
          "",
          "Review the files and lines this single commit changes. Anchor comments on the commit.",
          "",
          preamble(ref),
        ].join("\n"),
      }
    case "file":
      return {
        title: `Review: ${path}`,
        body: [
          `## Review the file \`${path}\` at ref \`${ref}\``,
          "",
          "Review the whole file: correctness, clarity, and anything risky. Anchor comments to its",
          `line ranges on ref \`${ref}\`.`,
          "",
          preamble(ref),
        ].join("\n"),
      }
    case "package":
      return {
        title: `Review: ${path}/`,
        body: [
          `## Review the package \`${path}\` at ref \`${ref}\``,
          "",
          "Review the files in this directory subtree. This is a broader pass — prioritise the most",
          `important findings. Anchor comments to file line ranges on ref \`${ref}\`.`,
          "",
          preamble(ref),
        ].join("\n"),
      }
  }
}

// reviewTargetNeedsPath reports whether a target requires a path (file/package).
export function reviewTargetNeedsPath(t: ReviewTarget): boolean {
  return t === "file" || t === "package"
}

// reviewTargetNeedsBase reports whether a target requires a base ref (diff).
export function reviewTargetNeedsBase(t: ReviewTarget): boolean {
  return t === "diff"
}
