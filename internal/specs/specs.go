// Package specs parses moongit's in-repo specifications: human-authored,
// high-altitude markdown documents stored under specs/ that describe what the
// code *should* do (the dual of the dex-derived Explore view, which describes
// what the code *is*).
//
// A spec is plain markdown with an optional YAML frontmatter block. The body
// follows a loose section convention — Intent / Behavior / Checklist /
// Non-goals — that downstream workflows (list endpoint, verify pre-pass, spec
// quality scoring) read structurally. This package is that shared reader: it
// splits frontmatter, extracts headings and checklist items, and surfaces
// 1-based line numbers so the web UI can deep-link to a section.
//
// Parsing is deliberately lenient. The list endpoint must still render a spec
// whose status is misspelled or whose alignment is out of range, so Parse only
// fails on malformed frontmatter YAML; semantic problems are reported
// separately by Validate, which linters and the quality workflow call.
package specs

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// Status is a spec's lifecycle state. An empty status is treated as living —
// most specs never bother to declare it.
type Status string

const (
	StatusLiving     Status = "living"
	StatusDraft      Status = "draft"
	StatusSuperseded Status = "superseded"
)

// Frontmatter is the optional YAML metadata block fenced by `---` at the top of
// a spec file. Every field is optional; a spec with no frontmatter at all is
// valid and parses to the zero value.
type Frontmatter struct {
	// ID is a stable slug for the spec, independent of its path (paths move;
	// links and verification stamps key off ID). Defaults to the filename stem.
	ID string `yaml:"id"`
	// Status is the lifecycle state — living, draft, or superseded.
	Status Status `yaml:"status"`
	// Owners lists the humans accountable for the spec (token names / handles).
	Owners []string `yaml:"owners"`
	// Covers lists globs (relative to repo root) the spec governs. The verify
	// pre-pass diffs these globs against a push to decide whether the spec might
	// have drifted. Empty means the spec claims no code surface.
	Covers []string `yaml:"covers"`
	// LastVerified is the date (YYYY-MM-DD) the verify agent last confirmed the
	// spec against the code. Stamped by the verify workflow; kept as a string so
	// round-tripping the frontmatter never reformats it.
	LastVerified string `yaml:"last_verified"`
	// Alignment is the verify agent's last code↔spec agreement score in [0,1].
	// A pointer so "never verified" (nil) is distinct from "0% aligned" (0.0).
	Alignment *float64 `yaml:"alignment"`
}

// Section is one heading in a spec body and the markdown beneath it, up to the
// next heading of the same or shallower level (so a section subsumes its
// subsections). Line is the 1-based line of the heading in the whole file,
// frontmatter included, for editor deep-links.
type Section struct {
	Title string
	Level int
	Body  string
	Line  int
}

// ChecklistItem is a GitHub-style task list line (`- [ ]` / `- [x]`) found
// anywhere in the body. Line is 1-based and file-absolute.
type ChecklistItem struct {
	Text    string
	Checked bool
	Line    int
}

// Spec is a fully parsed spec file.
type Spec struct {
	// Path is the repo-relative path, e.g. "specs/ssh-transport.md".
	Path string
	// Frontmatter holds the parsed metadata block (zero value if absent).
	Frontmatter Frontmatter
	// Title is the first H1 in the body, falling back to the filename stem.
	Title string
	// Body is the markdown after the frontmatter block.
	Body string
	// Sections are all headings in document order.
	Sections []Section
	// Checklist is every task-list item in the body, in document order.
	Checklist []ChecklistItem
}

// Parse reads a spec file. path is the repo-relative path (used for the ID/title
// fallback); data is the raw file. It returns an error only when a frontmatter
// block is present but is not valid YAML — every other input yields a best-effort
// Spec. Call Validate to surface semantic problems (bad status, out-of-range
// alignment).
func Parse(specPath string, data []byte) (*Spec, error) {
	fm, body, bodyStartLine, err := splitFrontmatter(data)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", specPath, err)
	}

	s := &Spec{Path: specPath, Frontmatter: fm, Body: body}
	s.Title, s.Sections, s.Checklist = analyze(body, bodyStartLine)
	if s.Title == "" {
		s.Title = titleFromPath(specPath)
	}
	if s.Frontmatter.ID == "" {
		s.Frontmatter.ID = idFromPath(specPath)
	}
	return s, nil
}

// Validate reports semantic problems with the spec's metadata. It is separate
// from Parse so rendering never depends on a spec being well-formed; linters and
// the quality workflow call it. A nil return means the metadata is sound.
func (s *Spec) Validate() error {
	var errs []error
	switch s.Frontmatter.Status {
	case "", StatusLiving, StatusDraft, StatusSuperseded:
	default:
		errs = append(errs, fmt.Errorf("status: unknown value %q (want living|draft|superseded)", s.Frontmatter.Status))
	}
	if a := s.Frontmatter.Alignment; a != nil && (*a < 0 || *a > 1) {
		errs = append(errs, fmt.Errorf("alignment: %v out of range [0,1]", *a))
	}
	return errors.Join(errs...)
}

// Section returns the first section whose title matches name case-insensitively
// (the convention sections — Intent, Behavior, Checklist, Non-goals — are
// referenced by name). The bool reports whether one was found.
func (s *Spec) Section(name string) (Section, bool) {
	for _, sec := range s.Sections {
		if strings.EqualFold(strings.TrimSpace(sec.Title), name) {
			return sec, true
		}
	}
	return Section{}, false
}

// splitFrontmatter separates a leading `---`-fenced YAML block from the body.
// A frontmatter block exists only when the very first line is exactly `---`;
// it ends at the next line that is exactly `---` or `...`. The returned
// bodyStartLine is the 1-based file line on which the body begins, so callers
// can report file-absolute line numbers.
func splitFrontmatter(data []byte) (Frontmatter, string, int, error) {
	s := string(data)
	// Normalize CRLF so delimiter matching and line splitting agree.
	s = strings.ReplaceAll(s, "\r\n", "\n")

	const fence = "---"
	if !strings.HasPrefix(s, fence+"\n") && s != fence {
		return Frontmatter{}, s, 1, nil
	}

	lines := strings.Split(s, "\n")
	// lines[0] is the opening fence; find the closing fence.
	end := -1
	for i := 1; i < len(lines); i++ {
		t := strings.TrimRight(lines[i], " \t")
		if t == fence || t == "..." {
			end = i
			break
		}
	}
	if end == -1 {
		// Unterminated fence — treat the whole file as body, no frontmatter.
		// (A lone `---` is a thematic break, not a broken spec.)
		return Frontmatter{}, s, 1, nil
	}

	yamlBlock := strings.Join(lines[1:end], "\n")
	var fm Frontmatter
	if strings.TrimSpace(yamlBlock) != "" {
		dec := yaml.NewDecoder(strings.NewReader(yamlBlock))
		if err := dec.Decode(&fm); err != nil {
			return Frontmatter{}, "", 0, fmt.Errorf("frontmatter: %w", err)
		}
	}

	body := strings.Join(lines[end+1:], "\n")
	bodyStartLine := end + 2 // closing fence is line end+1 (1-based); body follows.
	return fm, body, bodyStartLine, nil
}

// analyze walks the body once, tracking fenced-code state so headings and
// checklist markers inside ``` blocks are ignored. baseLine is the file-absolute
// 1-based line number of the body's first line.
func analyze(body string, baseLine int) (title string, sections []Section, checklist []ChecklistItem) {
	if body == "" {
		return "", nil, nil
	}
	lines := strings.Split(body, "\n")

	type openSection struct {
		idx       int
		bodyStart int // index into lines where this section's body begins
		level     int
	}
	var stack []openSection
	closeTo := func(level int, upto int) {
		for len(stack) > 0 && stack[len(stack)-1].level >= level {
			os := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			sections[os.idx].Body = strings.Trim(strings.Join(lines[os.bodyStart:upto], "\n"), "\n")
		}
	}

	inFence := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}

		if lvl, text, ok := parseHeading(line); ok {
			closeTo(lvl, i)
			if lvl == 1 && title == "" {
				title = text
			}
			sections = append(sections, Section{Title: text, Level: lvl, Line: baseLine + i})
			stack = append(stack, openSection{idx: len(sections) - 1, bodyStart: i + 1, level: lvl})
			continue
		}

		if text, checked, ok := parseChecklistItem(line); ok {
			checklist = append(checklist, ChecklistItem{Text: text, Checked: checked, Line: baseLine + i})
		}
	}
	closeTo(1, len(lines)) // close everything still open
	return title, sections, checklist
}

// parseHeading recognizes an ATX heading (`#`..`######` followed by a space).
// It returns the level, the trimmed heading text, and whether the line is one.
func parseHeading(line string) (level int, text string, ok bool) {
	// Leading indentation of 4+ spaces is a code block, not a heading.
	if strings.HasPrefix(line, "    ") {
		return 0, "", false
	}
	t := strings.TrimLeft(line, " ")
	n := 0
	for n < len(t) && t[n] == '#' {
		n++
	}
	if n == 0 || n > 6 || n >= len(t) || t[n] != ' ' {
		return 0, "", false
	}
	text = strings.TrimSpace(t[n+1:])
	// Drop an optional closing run of #'s ("## Title ##").
	text = strings.TrimRight(text, "#")
	text = strings.TrimSpace(text)
	return n, text, true
}

// parseChecklistItem recognizes a GitHub task-list line: a `-`, `*`, or `+`
// bullet, then `[ ]` or `[x]`, then the item text.
func parseChecklistItem(line string) (text string, checked, ok bool) {
	t := strings.TrimLeft(line, " \t")
	if len(t) < 5 {
		return "", false, false
	}
	if t[0] != '-' && t[0] != '*' && t[0] != '+' {
		return "", false, false
	}
	rest := strings.TrimLeft(t[1:], " ")
	if len(rest) < 3 || rest[0] != '[' || rest[2] != ']' {
		return "", false, false
	}
	switch rest[1] {
	case ' ':
		checked = false
	case 'x', 'X':
		checked = true
	default:
		return "", false, false
	}
	return strings.TrimSpace(rest[3:]), checked, true
}

// titleFromPath turns "specs/ssh-transport.md" into "Ssh Transport" — a
// readable fallback when a spec omits its H1.
func titleFromPath(p string) string {
	stem := idFromPath(p)
	words := strings.FieldsFunc(stem, func(r rune) bool { return r == '-' || r == '_' })
	for i, w := range words {
		if w == "" {
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// idFromPath returns the filename stem: "specs/ssh-transport.md" -> "ssh-transport".
func idFromPath(p string) string {
	base := path.Base(p)
	return strings.TrimSuffix(base, path.Ext(base))
}
