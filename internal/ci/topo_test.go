package ci

import (
	"testing"

	"gopkg.in/yaml.v3"
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

func TestJobStepMetaAndTranslateJobPlan(t *testing.T) {
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
	meta, err := JobStepMeta(p.Jobs["j"])
	if err != nil {
		t.Fatalf("JobStepMeta: %v", err)
	}
	if len(meta) != 2 {
		t.Fatalf("meta = %d, want 2", len(meta))
	}
	// run: sugar -> a provision shell step.
	if meta[0].Action != "shell" {
		t.Errorf("step 0 action = %q, want shell", meta[0].Action)
	}
	// Label is the human log caption: the command, not a generic action name
	// — so the UI shows "go test ./...", not "shell".
	if meta[0].Label != "go test ./..." {
		t.Errorf("step 0 label = %q, want %q", meta[0].Label, "go test ./...")
	}
	// mooncake's `cmd: {argv: [...]}` is rewritten to provision's bare
	// sequence `cmd: [...]` (the argv: wrapper drops, docs/ops-
	// provisioning.md's translation table) — action stays "cmd", and the
	// label is now the real command rather than the old generic "cmd".
	if meta[1].Action != "cmd" {
		t.Errorf("step 1 action = %q, want cmd", meta[1].Action)
	}
	if meta[1].Label != "go vet ./..." {
		t.Errorf("step 1 label = %q, want %q", meta[1].Label, "go vet ./...")
	}

	planYAML, err := TranslateJobPlan(p.Jobs["j"])
	if err != nil {
		t.Fatalf("TranslateJobPlan: %v", err)
	}
	var steps []map[string]any
	if err := yaml.Unmarshal(planYAML, &steps); err != nil {
		t.Fatalf("translated plan is not a step list: %v\n%s", err, planYAML)
	}
	if len(steps) != 2 {
		t.Fatalf("plan steps = %d, want 2\n%s", len(steps), planYAML)
	}
	if steps[0]["shell"] != "go test ./..." {
		t.Errorf("step 0 shell = %v, want %q", steps[0]["shell"], "go test ./...")
	}
	cmd, ok := steps[1]["cmd"].([]any)
	if !ok || len(cmd) != 3 || cmd[0] != "go" || cmd[1] != "vet" || cmd[2] != "./..." {
		t.Errorf("step 1 cmd = %#v, want [go vet ./...]", steps[1]["cmd"])
	}
	// Every emitted shell/cmd step needs an idempotency gate or `validate
	// --strict` fails it (provision spec §6.1) — CI steps are exit-code-is-
	// the-contract, not state changes.
	if steps[0]["changed_when"] != "false" || steps[1]["changed_when"] != "false" {
		t.Errorf("steps missing changed_when: false\n%s", planYAML)
	}
}
