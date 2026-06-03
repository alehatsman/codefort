package specs

import (
	"strings"
	"testing"
)

func TestComputeAlignment(t *testing.T) {
	cases := []struct {
		name    string
		markers []LineMarker
		want    float64
	}{
		{"none verifiable → 1.0", []LineMarker{{Marker: MarkerUnverifiable}, {Marker: MarkerUnspecced}}, 1.0},
		{"empty → 1.0", nil, 1.0},
		{"all aligned", []LineMarker{{Marker: MarkerAligned}, {Marker: MarkerAligned}}, 1.0},
		{"half drifted", []LineMarker{{Marker: MarkerAligned}, {Marker: MarkerDrifted}}, 0.5},
		{
			"unverifiable excluded",
			[]LineMarker{{Marker: MarkerAligned}, {Marker: MarkerDrifted}, {Marker: MarkerUnverifiable}},
			0.5,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ComputeAlignment(tc.markers); got != tc.want {
				t.Errorf("ComputeAlignment = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMarkerValid(t *testing.T) {
	for _, m := range []Marker{MarkerAligned, MarkerDrifted, MarkerUnverifiable, MarkerUnspecced} {
		if !m.Valid() {
			t.Errorf("%q should be valid", m)
		}
	}
	if Marker("bogus").Valid() {
		t.Error("bogus marker should be invalid")
	}
}

func TestStampUpsertsExistingFrontmatter(t *testing.T) {
	src := `---
id: ssh-transport
status: living
last_verified: 2026-01-01
alignment: 0.50
---
# SSH Transport
## Intent
Body stays put.
`
	out := string(Stamp([]byte(src), "2026-06-03", 0.913))
	// Existing keys are updated in place (not duplicated).
	if strings.Count(out, "last_verified:") != 1 {
		t.Errorf("last_verified duplicated:\n%s", out)
	}
	if strings.Count(out, "alignment:") != 1 {
		t.Errorf("alignment duplicated:\n%s", out)
	}
	if !strings.Contains(out, "last_verified: 2026-06-03") {
		t.Errorf("last_verified not updated:\n%s", out)
	}
	if !strings.Contains(out, "alignment: 0.91") {
		t.Errorf("alignment not updated/rounded:\n%s", out)
	}
	// Other fields + body survive.
	if !strings.Contains(out, "id: ssh-transport") || !strings.Contains(out, "Body stays put.") {
		t.Errorf("frontmatter/body not preserved:\n%s", out)
	}

	// The stamped output re-parses with the new values.
	spec, err := Parse("specs/ssh-transport.md", []byte(out))
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if spec.Frontmatter.LastVerified != "2026-06-03" {
		t.Errorf("parsed last_verified = %q", spec.Frontmatter.LastVerified)
	}
	if spec.Frontmatter.Alignment == nil || *spec.Frontmatter.Alignment != 0.91 {
		t.Errorf("parsed alignment = %v", spec.Frontmatter.Alignment)
	}
}

func TestStampInsertsKeysAndBlock(t *testing.T) {
	// Frontmatter exists but lacks the verify keys → they're appended.
	withFM := "---\nid: x\n---\n# X\nbody\n"
	out := string(Stamp([]byte(withFM), "2026-06-03", 1.0))
	if !strings.Contains(out, "id: x") || !strings.Contains(out, "last_verified: 2026-06-03") ||
		!strings.Contains(out, "alignment: 1.00") {
		t.Errorf("keys not inserted into existing frontmatter:\n%s", out)
	}

	// No frontmatter at all → a block is prepended, body preserved.
	noFM := "# Title\njust body\n"
	out = string(Stamp([]byte(noFM), "2026-06-03", 0.0))
	if !strings.HasPrefix(out, "---\n") {
		t.Errorf("frontmatter block not prepended:\n%s", out)
	}
	spec, err := Parse("specs/x.md", []byte(out))
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if spec.Title != "Title" {
		t.Errorf("title lost: %q", spec.Title)
	}
	if spec.Frontmatter.Alignment == nil || *spec.Frontmatter.Alignment != 0 {
		t.Errorf("alignment = %v, want 0", spec.Frontmatter.Alignment)
	}
}

// Regression for #304: appending the verify keys to existing frontmatter must
// not clobber the closing fence or the first body line via slice aliasing, and
// must not duplicate the keys. The stamped output has to round-trip through
// Parse with frontmatter and body intact.
func TestStampAppendPreservesFenceAndBody(t *testing.T) {
	src := "---\nid: agent-runs\nstatus: draft\nowners: [aleh]\ncovers:\n  - \"internal/server/agent.go\"\n---\n# Agent Runs\n\n## Intent\nbody text\n"
	out := string(Stamp([]byte(src), "2026-06-03", 0.82))

	if got := strings.Count(out, "last_verified:"); got != 1 {
		t.Errorf("last_verified appears %d times, want 1:\n%s", got, out)
	}
	if got := strings.Count(out, "alignment:"); got != 1 {
		t.Errorf("alignment appears %d times, want 1:\n%s", got, out)
	}
	// Two fences: opening + closing. Slice aliasing used to eat the closing one.
	if got := strings.Count(out, "---"); got != 2 {
		t.Errorf("fence count = %d, want 2 (open+close):\n%s", got, out)
	}
	if !strings.Contains(out, "# Agent Runs") {
		t.Errorf("body heading clobbered:\n%s", out)
	}

	spec, err := Parse("specs/agent-runs.md", []byte(out))
	if err != nil {
		t.Fatalf("stamped spec does not re-parse: %v\n%s", err, out)
	}
	if spec.Frontmatter.Status != "draft" {
		t.Errorf("status = %q, want draft (frontmatter delimiting broke)", spec.Frontmatter.Status)
	}
	if spec.Frontmatter.LastVerified != "2026-06-03" {
		t.Errorf("last_verified = %q, want 2026-06-03", spec.Frontmatter.LastVerified)
	}
	if spec.Title != "Agent Runs" {
		t.Errorf("title = %q, want Agent Runs", spec.Title)
	}
}
