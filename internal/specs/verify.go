package specs

import (
	"fmt"
	"strconv"
	"strings"
)

// Marker is the per-line verification verdict: whether a spec's stated behavior
// or checklist line is borne out by the code as it stands. It captures spec↔code
// AGREEMENT only — a fully "aligned" spec can still be the wrong spec. Whether a
// spec is *good* is judged separately (quality scoring, #231); passing
// verification never proves the spec is correct, only that the code matches it.
type Marker string

const (
	// MarkerAligned: the code bears out the spec line.
	MarkerAligned Marker = "aligned"
	// MarkerDrifted: the code contradicts the spec line (spec or code is stale).
	MarkerDrifted Marker = "drifted"
	// MarkerUnverifiable: the line can't be confirmed or refuted from the code.
	MarkerUnverifiable Marker = "unverifiable"
	// MarkerUnspecced: observed code behavior the spec doesn't cover (a gap).
	MarkerUnspecced Marker = "unspecced"
)

// Valid reports whether m is one of the known markers.
func (m Marker) Valid() bool {
	switch m {
	case MarkerAligned, MarkerDrifted, MarkerUnverifiable, MarkerUnspecced:
		return true
	}
	return false
}

// LineMarker attributes a verdict to a spec line. Line is 1-based and
// file-absolute (the same numbering Section/ChecklistItem use), so the web
// truth gutter can render the marker beside the source line. Text echoes the
// line for context; Note is the verifier's brief rationale.
type LineMarker struct {
	Line   int    `json:"line"`
	Text   string `json:"text,omitempty"`
	Marker Marker `json:"marker"`
	Note   string `json:"note,omitempty"`
}

// VerificationResult is one verify pass over a spec. Alignment is the overall
// code↔spec agreement in [0,1]; Markers are the per-line verdicts; Conflicts
// names the drifted points in prose; Notes is free-form verifier commentary.
// It is a MATCH report, not a correctness claim (see Marker).
type VerificationResult struct {
	Alignment float64      `json:"alignment"`
	Markers   []LineMarker `json:"markers"`
	Conflicts []string     `json:"conflicts,omitempty"`
	Notes     string       `json:"notes,omitempty"`
}

// ComputeAlignment is the fraction of *verifiable* lines (aligned or drifted)
// that are aligned — unverifiable and unspecced lines are excluded so they
// neither inflate nor deflate the score. Returns 1.0 when nothing is verifiable
// (no claims to contradict), matching "no drift detected".
func ComputeAlignment(markers []LineMarker) float64 {
	aligned, verifiable := 0, 0
	for _, m := range markers {
		switch m.Marker {
		case MarkerAligned:
			aligned++
			verifiable++
		case MarkerDrifted:
			verifiable++
		}
	}
	if verifiable == 0 {
		return 1.0
	}
	return float64(aligned) / float64(verifiable)
}

// Stamp returns content with last_verified and alignment set in its YAML
// frontmatter — the at-a-glance result the verify workflow commits back to the
// repo (the durable, queryable history lives in the spec_verifications table).
// It upserts the two keys line-by-line so every other frontmatter field and the
// body are preserved verbatim, and it prepends a fresh frontmatter block when
// the spec has none. lastVerified is a YYYY-MM-DD date; alignment is written
// with two decimals.
func Stamp(content []byte, lastVerified string, alignment float64) []byte {
	align := strconv.FormatFloat(roundedAlignment(alignment), 'f', 2, 64)
	text := strings.ReplaceAll(string(content), "\r\n", "\n")
	lines := strings.Split(text, "\n")

	const fence = "---"
	hasFM := len(lines) > 0 && lines[0] == fence
	if !hasFM {
		block := []string{fence, "last_verified: " + lastVerified, "alignment: " + align, fence}
		return []byte(strings.Join(append(block, lines...), "\n"))
	}

	end := -1
	for i := 1; i < len(lines); i++ {
		if t := strings.TrimRight(lines[i], " \t"); t == fence || t == "..." {
			end = i
			break
		}
	}
	if end == -1 {
		// Opening fence with no close: not a real frontmatter block, prepend one.
		block := []string{fence, "last_verified: " + lastVerified, "alignment: " + align, fence}
		return []byte(strings.Join(append(block, lines...), "\n"))
	}

	// Three-index slice caps fmLines at `end` so upsertYAML's append (the
	// key-absent path) allocates a fresh backing array instead of overwriting
	// the closing fence + first body line in the shared `lines` array.
	fmLines := lines[1:end:end]
	fmLines = upsertYAML(fmLines, "last_verified", lastVerified)
	fmLines = upsertYAML(fmLines, "alignment", align)

	out := make([]string, 0, len(lines)+2)
	out = append(out, fence)
	out = append(out, fmLines...)
	out = append(out, lines[end:]...) // closing fence + body
	return []byte(strings.Join(out, "\n"))
}

// upsertYAML replaces the value of a top-level scalar key, or appends the pair
// when the key is absent. Matching is on the exact "key:" prefix of the trimmed
// line so it never collides with a longer key that shares the prefix.
func upsertYAML(lines []string, key, value string) []string {
	prefix := key + ":"
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == prefix || strings.HasPrefix(t, prefix+" ") {
			lines[i] = fmt.Sprintf("%s: %s", key, value)
			return lines
		}
	}
	return append(lines, fmt.Sprintf("%s: %s", key, value))
}

// roundedAlignment clamps to [0,1] and rounds to two decimals so a stray
// out-of-range or long-tail value can't land in the frontmatter.
func roundedAlignment(a float64) float64 {
	if a < 0 {
		a = 0
	}
	if a > 1 {
		a = 1
	}
	return float64(int(a*100+0.5)) / 100
}
