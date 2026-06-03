package specs

import (
	"testing"
)

func TestParseFrontmatter(t *testing.T) {
	src := `---
id: ssh-transport
status: living
owners: [aleh, agent#1]
covers: ["internal/ssh/**", "cmd/moongitd/**"]
last_verified: 2026-06-02
alignment: 0.91
---
# SSH Transport
## Intent
Git over SSH as an opt-in second port.
`
	s, err := Parse("specs/ssh-transport.md", []byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	fm := s.Frontmatter
	if fm.ID != "ssh-transport" {
		t.Errorf("ID = %q, want ssh-transport", fm.ID)
	}
	if fm.Status != StatusLiving {
		t.Errorf("Status = %q, want living", fm.Status)
	}
	if len(fm.Owners) != 2 || fm.Owners[0] != "aleh" {
		t.Errorf("Owners = %v", fm.Owners)
	}
	if len(fm.Covers) != 2 || fm.Covers[0] != "internal/ssh/**" {
		t.Errorf("Covers = %v", fm.Covers)
	}
	if fm.LastVerified != "2026-06-02" {
		t.Errorf("LastVerified = %q, want 2026-06-02", fm.LastVerified)
	}
	if fm.Alignment == nil || *fm.Alignment != 0.91 {
		t.Errorf("Alignment = %v, want 0.91", fm.Alignment)
	}
	if s.Title != "SSH Transport" {
		t.Errorf("Title = %q, want SSH Transport", s.Title)
	}
	if err := s.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestParseNoFrontmatter(t *testing.T) {
	src := "# Title Only\n\nJust body, no metadata.\n"
	s, err := Parse("specs/plain.md", []byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if s.Frontmatter.Status != "" {
		t.Errorf("Status = %q, want empty", s.Frontmatter.Status)
	}
	// ID falls back to the filename stem.
	if s.Frontmatter.ID != "plain" {
		t.Errorf("ID = %q, want plain", s.Frontmatter.ID)
	}
	if s.Title != "Title Only" {
		t.Errorf("Title = %q", s.Title)
	}
	if s.Body != src {
		t.Errorf("Body should equal whole file when no frontmatter:\n%q", s.Body)
	}
}

func TestTitleAndIDFallback(t *testing.T) {
	// No H1, no frontmatter id -> both derive from the path.
	s, err := Parse("specs/two-machine-deploy.md", []byte("## Intent\nstuff\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if s.Frontmatter.ID != "two-machine-deploy" {
		t.Errorf("ID = %q", s.Frontmatter.ID)
	}
	if s.Title != "Two Machine Deploy" {
		t.Errorf("Title = %q, want Two Machine Deploy", s.Title)
	}
}

func TestSections(t *testing.T) {
	src := `# Spec
## Intent
The why.
## Behavior
WHEN pushed THEN verify.
### Edge cases
nested under Behavior.
## Non-goals
not this.
`
	s, err := Parse("specs/x.md", []byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// Headings: H1 Spec, H2 Intent, H2 Behavior, H3 Edge cases, H2 Non-goals.
	if len(s.Sections) != 5 {
		t.Fatalf("len(Sections) = %d, want 5: %+v", len(s.Sections), s.Sections)
	}
	intent, ok := s.Section("intent")
	if !ok {
		t.Fatal("Section(intent) not found")
	}
	if intent.Body != "The why." {
		t.Errorf("Intent body = %q", intent.Body)
	}
	beh, _ := s.Section("Behavior")
	// A section subsumes its subsections.
	if beh.Body != "WHEN pushed THEN verify.\n### Edge cases\nnested under Behavior." {
		t.Errorf("Behavior body = %q", beh.Body)
	}
	if beh.Level != 2 {
		t.Errorf("Behavior level = %d, want 2", beh.Level)
	}
}

func TestSectionLineNumbers(t *testing.T) {
	// Frontmatter occupies lines 1-3; body H1 is line 4.
	src := `---
id: x
---
# Title
## Intent
body
`
	s, err := Parse("specs/x.md", []byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if s.Sections[0].Title != "Title" || s.Sections[0].Line != 4 {
		t.Errorf("H1 = %q@%d, want Title@4", s.Sections[0].Title, s.Sections[0].Line)
	}
	if s.Sections[1].Title != "Intent" || s.Sections[1].Line != 5 {
		t.Errorf("H2 = %q@%d, want Intent@5", s.Sections[1].Title, s.Sections[1].Line)
	}
}

func TestChecklist(t *testing.T) {
	src := `# Spec
## Checklist
- [ ] unchecked
- [x] checked lower
- [X] checked upper
* [ ] star bullet
  - [ ] indented
- not a checklist item
`
	s, err := Parse("specs/x.md", []byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(s.Checklist) != 5 {
		t.Fatalf("len(Checklist) = %d, want 5: %+v", len(s.Checklist), s.Checklist)
	}
	if s.Checklist[0].Checked || s.Checklist[0].Text != "unchecked" {
		t.Errorf("item 0 = %+v", s.Checklist[0])
	}
	if !s.Checklist[1].Checked || s.Checklist[1].Text != "checked lower" {
		t.Errorf("item 1 = %+v", s.Checklist[1])
	}
	if !s.Checklist[2].Checked {
		t.Errorf("item 2 (uppercase X) should be checked")
	}
}

func TestCodeFenceIgnored(t *testing.T) {
	// Headings and checklist markers inside a fence are not structure.
	src := "# Spec\n## Intent\n```\n# not a heading\n- [ ] not a checklist item\n```\n## Real\ntext\n"
	s, err := Parse("specs/x.md", []byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	for _, sec := range s.Sections {
		if sec.Title == "not a heading" {
			t.Errorf("heading inside code fence was parsed: %+v", s.Sections)
		}
	}
	if len(s.Checklist) != 0 {
		t.Errorf("checklist item inside fence was parsed: %+v", s.Checklist)
	}
	// The real heading after the fence still parses.
	if _, ok := s.Section("Real"); !ok {
		t.Error("real heading after fence not found")
	}
}

func TestValidate(t *testing.T) {
	bad := float64(1.5)
	cases := []struct {
		name    string
		fm      Frontmatter
		wantErr bool
	}{
		{"empty status ok", Frontmatter{}, false},
		{"living ok", Frontmatter{Status: StatusLiving}, false},
		{"bogus status", Frontmatter{Status: "archived"}, true},
		{"alignment in range", Frontmatter{Alignment: ptr(0.5)}, false},
		{"alignment out of range", Frontmatter{Alignment: &bad}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Spec{Frontmatter: tc.fm}
			err := s.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestParseBadFrontmatterYAML(t *testing.T) {
	// A present-but-broken frontmatter block is the one case Parse errors on.
	src := "---\nid: [unterminated\n---\n# Body\n"
	if _, err := Parse("specs/x.md", []byte(src)); err == nil {
		t.Fatal("Parse should error on malformed frontmatter YAML")
	}
}

func TestUnterminatedFenceIsBody(t *testing.T) {
	// A leading `---` with no closing fence is a thematic break, not frontmatter.
	src := "---\n# Heading after a rule\n"
	s, err := Parse("specs/x.md", []byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if s.Frontmatter.ID != "x" { // fell back to path, i.e. no frontmatter consumed
		t.Errorf("ID = %q, want x (no frontmatter)", s.Frontmatter.ID)
	}
	if s.Body != src {
		t.Errorf("Body = %q, want whole file", s.Body)
	}
}

func TestCRLF(t *testing.T) {
	src := "---\r\nstatus: draft\r\n---\r\n# Title\r\n## Intent\r\nbody\r\n"
	s, err := Parse("specs/x.md", []byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if s.Frontmatter.Status != StatusDraft {
		t.Errorf("Status = %q, want draft", s.Frontmatter.Status)
	}
	if s.Title != "Title" {
		t.Errorf("Title = %q", s.Title)
	}
}

func ptr(f float64) *float64 { return &f }
