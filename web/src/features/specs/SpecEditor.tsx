import { useState } from "react"
import { useMutation } from "@tanstack/react-query"
import { Link } from "react-router-dom"
import { api } from "@/api/client"
import Markdown from "@/shell/Markdown"
import { Button, ErrorMessage } from "@/ui"
import type { WriteSpecResult } from "@/api/types"

interface Props {
  owner: string
  repo: string
  /** Repo-relative spec path, e.g. "specs/ssh-transport.md". */
  path: string
  /** The spec's raw file content (frontmatter included) to edit. */
  initialContent: string
  onDone: () => void
}

/**
 * Split-pane spec editor: a plain textarea on the left, live markdown preview
 * on the right (frontmatter stripped, like the read view). Save commits to a
 * feature branch via the write endpoint and surfaces the branch + a link to
 * open a PR — specs reach main through review, never a direct push. A richer
 * editor (CodeMirror) is a clean drop-in later; a textarea keeps the dep
 * surface flat and is plenty for prose specs.
 */
export default function SpecEditor({ owner, repo, path, initialContent, onDone }: Props) {
  const [content, setContent] = useState(initialContent)
  const save = useMutation<WriteSpecResult, Error, void>({
    mutationFn: () => api.writeSpec(owner, repo, path, { content }),
  })
  const basePath = path.split("/").slice(0, -1).join("/")
  const dirty = content !== initialContent

  return (
    <div className="spec-editor">
      <div className="spec-editor__panes">
        <textarea
          className="spec-editor__input"
          value={content}
          onChange={(e) => setContent(e.target.value)}
          spellCheck={false}
          aria-label="Spec markdown"
        />
        <div className="spec-editor__preview">
          <Markdown
            content={stripFrontmatter(content)}
            owner={owner}
            repo={repo}
            basePath={basePath}
          />
        </div>
      </div>

      <div className="spec-editor__bar">
        <Button variant="primary" onClick={() => save.mutate()} disabled={!dirty || save.isPending}>
          {save.isPending ? "Saving…" : "Save to branch"}
        </Button>
        <Button variant="ghost" onClick={onDone}>
          Done
        </Button>
        <ErrorMessage error={save.error} />
        {save.data && <SaveResult owner={owner} repo={repo} result={save.data} />}
      </div>
    </div>
  )
}

function SaveResult({
  owner,
  repo,
  result,
}: {
  owner: string
  repo: string
  result: WriteSpecResult
}) {
  return (
    <span className="spec-editor__saved muted small">
      Committed to <code>{result.branch}</code> ({result.commit.slice(0, 8)}).{" "}
      <Link to={`/${owner}/${repo}/compare?head=${encodeURIComponent(result.branch)}`}>
        Open a pull request →
      </Link>
    </span>
  )
}

// stripFrontmatter drops a leading `---` … `---` YAML block so the preview shows
// the spec body, matching the read view (which renders SpecContent.body). Mirror
// of the server parser's split, kept deliberately simple.
function stripFrontmatter(src: string): string {
  const text = src.replace(/\r\n/g, "\n")
  const lines = text.split("\n")
  if (lines[0] !== "---") return src
  for (let i = 1; i < lines.length; i++) {
    if (lines[i] === "---" || lines[i] === "...") {
      return lines.slice(i + 1).join("\n")
    }
  }
  return src
}
