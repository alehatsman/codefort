package main

import (
	"testing"

	"github.com/alehatsman/codefort/internal/specs"
)

func TestParseVerifyResultFencedBlock(t *testing.T) {
	out := "Here's my analysis.\n\n```json\n" +
		`{"alignment":0.5,"markers":[{"line":6,"text":"WHEN x","marker":"aligned","note":"server.go:10"},{"line":8,"marker":"drifted","note":"gone"}],"conflicts":["line 8 gone"],"notes":"ok"}` +
		"\n```\nDone."
	res, err := parseVerifyResult(out)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(res.Markers) != 2 {
		t.Fatalf("markers = %d, want 2", len(res.Markers))
	}
	if res.Markers[0].Marker != specs.MarkerAligned || res.Markers[0].Line != 6 {
		t.Errorf("marker0 = %+v", res.Markers[0])
	}
	if res.Markers[1].Marker != specs.MarkerDrifted {
		t.Errorf("marker1 = %+v", res.Markers[1])
	}
	if len(res.Conflicts) != 1 || res.Notes != "ok" {
		t.Errorf("conflicts/notes = %v / %q", res.Conflicts, res.Notes)
	}
}

func TestParseVerifyResultBareObject(t *testing.T) {
	// No fence, prose around a bare object.
	out := `The result: {"alignment":1.0,"markers":[]} — looks aligned.`
	res, err := parseVerifyResult(out)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.Alignment != 1.0 || len(res.Markers) != 0 {
		t.Errorf("res = %+v", res)
	}
}

func TestParseVerifyResultErrors(t *testing.T) {
	for _, in := range []string{"", "no json here at all", "```json\nnot valid json\n```"} {
		if _, err := parseVerifyResult(in); err == nil {
			t.Errorf("expected error for %q", in)
		}
	}
}

func TestBranchSafe(t *testing.T) {
	cases := map[string]string{
		"ssh-transport":    "ssh-transport",
		"ci/pipeline":      "ci-pipeline",
		"weird id!@# here": "weird-id-here",
		"":                 "spec",
		"--x--":            "x",
	}
	for in, want := range cases {
		if got := branchSafe(in); got != want {
			t.Errorf("branchSafe(%q) = %q, want %q", in, got, want)
		}
	}
}
