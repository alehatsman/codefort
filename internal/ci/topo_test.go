package ci

import (
	"strings"
	"testing"
)

func TestTopoOrderRespectsNeeds(t *testing.T) {
	p, err := Parse([]byte(`
version: "1"
jobs:
  deploy:
    needs: [test]
    steps: [{run: echo deploy}]
  test:
    needs: [build]
    steps: [{run: echo test}]
  build:
    steps: [{run: echo build}]
  lint:
    steps: [{run: echo lint}]
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	order, err := p.TopoOrder()
	if err != nil {
		t.Fatalf("TopoOrder: %v", err)
	}
	pos := map[string]int{}
	for i, n := range order {
		pos[n] = i
	}
	if len(order) != 4 {
		t.Fatalf("order = %v, want 4 jobs", order)
	}
	// Each job must come after all of its needs.
	if !(pos["build"] < pos["test"] && pos["test"] < pos["deploy"]) {
		t.Errorf("order violates needs: %v", order)
	}
	// Deterministic: independent jobs (build, lint) emitted in sorted order.
	if pos["build"] > pos["lint"] {
		t.Errorf("ties should break by sorted name; got %v", order)
	}
}

func TestMooncakeStepsTranslation(t *testing.T) {
	p, err := Parse([]byte(`
version: "1"
jobs:
  j:
    steps:
      - run: go test ./...
      - cmd: { argv: ["go", "vet", "./..."] }
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	steps, err := MooncakeSteps(p.Jobs["j"])
	if err != nil {
		t.Fatalf("MooncakeSteps: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(steps))
	}
	// run: sugar -> shell action; YAML carries the cmd.
	if steps[0].Action != "shell" {
		t.Errorf("step 0 action = %q, want shell", steps[0].Action)
	}
	if !strings.Contains(steps[0].YAML, "go test ./...") {
		t.Errorf("step 0 YAML missing cmd:\n%s", steps[0].YAML)
	}
	// raw step passes through; action is its top-level key.
	if steps[1].Action != "cmd" {
		t.Errorf("step 1 action = %q, want cmd", steps[1].Action)
	}
	// Label is the human log caption: the command for a run: step, the action
	// otherwise — so the UI shows "go test ./...", not a generic "shell".
	if steps[0].Label != "go test ./..." {
		t.Errorf("step 0 label = %q, want %q", steps[0].Label, "go test ./...")
	}
	if steps[1].Label != "cmd" {
		t.Errorf("step 1 label = %q, want cmd", steps[1].Label)
	}
}
