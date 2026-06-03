package ci

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOnMatches(t *testing.T) {
	cases := []struct {
		name     string
		branches []string
		ref      string
		want     bool
	}{
		{"empty filter matches any branch", nil, "refs/heads/anything", true},
		{"empty filter matches a tag too", nil, "refs/tags/v1", true},
		{"exact branch match", []string{"main"}, "refs/heads/main", true},
		{"exact branch miss", []string{"main"}, "refs/heads/feat-x", false},
		{"glob matches one segment", []string{"feat/*"}, "refs/heads/feat/x", true},
		{"glob does not cross slash", []string{"feat/*"}, "refs/heads/feat/x/y", false},
		{"one of several patterns", []string{"main", "feat/*"}, "refs/heads/feat/login", true},
		{"non-branch ref never matches a filter", []string{"main"}, "refs/tags/main", false},
		{"filter set but branch unlisted", []string{"main", "release/*"}, "refs/heads/chore/x", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			on := On{Push: Push{Branches: tc.branches}}
			if got := on.Matches(tc.ref); got != tc.want {
				t.Errorf("On{%v}.Matches(%q) = %v, want %v", tc.branches, tc.ref, got, tc.want)
			}
		})
	}
}

// exampleYAML mirrors the spec in issue #14: version, push branch filter, two
// jobs, `run:` sugar, a raw mooncake `cmd` step, and an explicit empty needs.
const exampleYAML = `
version: "1"
on:
  push:
    branches: [main, "feat/*"]
jobs:
  test:
    steps:
      - run: go build ./...
      - run: go test ./...
  lint:
    needs: []
    steps:
      - run: gofmt -l .
      - cmd: { argv: ["go", "vet", "./..."] }
`

func TestParseExample(t *testing.T) {
	p, err := Parse([]byte(exampleYAML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if p.Version != "1" {
		t.Errorf("version = %q, want %q", p.Version, "1")
	}
	// The `on:` key is a YAML 1.1 boolean footgun; confirm it decodes as the
	// push filter and not a stray bool key.
	if got := p.On.Push.Branches; len(got) != 2 || got[0] != "main" || got[1] != "feat/*" {
		t.Errorf("on.push.branches = %v, want [main feat/*]", got)
	}
	if len(p.Jobs) != 2 {
		t.Fatalf("jobs = %d, want 2", len(p.Jobs))
	}
	if len(p.Jobs["test"].Steps) != 2 {
		t.Errorf("test steps = %d, want 2", len(p.Jobs["test"].Steps))
	}
	if names := p.JobNames(); len(names) != 2 || names[0] != "lint" || names[1] != "test" {
		t.Errorf("JobNames = %v, want [lint test]", names)
	}
}

func TestParseJobImage(t *testing.T) {
	const withImage = `
version: "1"
jobs:
  build:
    image: golang:1.24
    steps:
      - run: go build ./...
  test:
    steps:
      - run: go test ./...
`
	p, err := Parse([]byte(withImage))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := p.Jobs["build"].Image; got != "golang:1.24" {
		t.Errorf("build job image = %q, want %q", got, "golang:1.24")
	}
	// An omitted image stays empty so the runner falls back to the server default.
	if got := p.Jobs["test"].Image; got != "" {
		t.Errorf("test job image = %q, want empty", got)
	}
}

func TestTranslateRunSugar(t *testing.T) {
	p, err := Parse([]byte(exampleYAML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	got, err := TranslateJob(p.Jobs["test"])
	if err != nil {
		t.Fatalf("TranslateJob: %v", err)
	}

	// Output must be a top-level sequence (mooncake rejects a tasks: map), and
	// each `run:` must have become a shell step with a cmd.
	var steps []map[string]any
	if err := yaml.Unmarshal(got, &steps); err != nil {
		t.Fatalf("translated output is not a step list: %v\n%s", err, got)
	}
	if len(steps) != 2 {
		t.Fatalf("steps = %d, want 2\n%s", len(steps), got)
	}
	shell, ok := steps[0]["shell"].(map[string]any)
	if !ok {
		t.Fatalf("step 0 is not a shell step: %#v", steps[0])
	}
	if shell["cmd"] != "go build ./..." {
		t.Errorf("step 0 cmd = %q, want %q", shell["cmd"], "go build ./...")
	}
}

func TestTranslateRawStepPassthrough(t *testing.T) {
	p, err := Parse([]byte(exampleYAML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	got, err := TranslateJob(p.Jobs["lint"])
	if err != nil {
		t.Fatalf("TranslateJob: %v", err)
	}

	var steps []map[string]any
	if err := yaml.Unmarshal(got, &steps); err != nil {
		t.Fatalf("translated output is not a step list: %v\n%s", err, got)
	}
	// lint: step 0 is `run: gofmt -l .` (sugar -> shell), step 1 is a raw
	// `cmd` step that must pass through untouched.
	if _, ok := steps[0]["shell"]; !ok {
		t.Errorf("step 0 should be a shell step: %#v", steps[0])
	}
	cmd, ok := steps[1]["cmd"].(map[string]any)
	if !ok {
		t.Fatalf("step 1 raw cmd step not preserved: %#v", steps[1])
	}
	argv, ok := cmd["argv"].([]any)
	if !ok || len(argv) != 3 || argv[0] != "go" {
		t.Errorf("step 1 argv = %#v, want [go vet ./...]", cmd["argv"])
	}
}

func TestParseValidationErrors(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "wrong version",
			yaml: `version: "2"` + "\n" + `jobs: {a: {steps: [{run: "x"}]}}`,
			want: "version",
		},
		{
			name: "no jobs",
			yaml: `version: "1"` + "\n" + `jobs: {}`,
			want: "at least one job",
		},
		{
			name: "job without steps",
			yaml: `version: "1"` + "\n" + `jobs: {a: {steps: []}}`,
			want: "at least one step",
		},
		{
			name: "needs unknown job",
			yaml: `version: "1"` + "\n" + `jobs: {a: {needs: [ghost], steps: [{run: "x"}]}}`,
			want: `unknown job "ghost"`,
		},
		{
			name: "dependency cycle",
			yaml: `version: "1"` + "\n" + `jobs: {a: {needs: [b], steps: [{run: x}]}, b: {needs: [a], steps: [{run: y}]}}`,
			want: "cycle",
		},
		{
			name: "run combined with other keys",
			yaml: `version: "1"` + "\n" + `jobs: {a: {steps: [{run: x, cmd: {argv: [y]}}]}}`,
			want: "`run` cannot be combined",
		},
		{
			name: "empty run",
			yaml: `version: "1"` + "\n" + `jobs: {a: {steps: [{run: ""}]}}`,
			want: "non-empty command",
		},
		{
			name: "unknown top-level key",
			yaml: `version: "1"` + "\n" + `jobz: {a: {steps: [{run: x}]}}`,
			want: "field jobz",
		},
		{
			name: "step typo (step not steps)",
			yaml: `version: "1"` + "\n" + `jobs: {a: {step: [{run: x}]}}`,
			want: "field step",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.yaml))
			if err == nil {
				t.Fatalf("Parse: expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want substring %q", err.Error(), tc.want)
			}
		})
	}
}

func TestToolchainHints(t *testing.T) {
	p, err := Parse([]byte(`
version: "1"
jobs:
  build:
    steps:
      - run: go build ./...
  web:
    steps:
      - run: cd web && npm ci && npm run build
  pinned:
    image: my-go:latest
    steps:
      - run: go test ./...
  shell-only:
    steps:
      - run: echo "hello"
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	hints := ToolchainHints(p)

	got := map[string]string{}
	for _, h := range hints {
		got[h.Job] = h.Tool
	}
	// build (go) and web (npm, found past the leading `cd`) warn; pinned has an
	// explicit image: so it's skipped despite using go; shell-only uses no
	// toolchain.
	if got["build"] != "go" {
		t.Errorf("build hint = %q, want go", got["build"])
	}
	if got["web"] != "npm" {
		t.Errorf("web hint = %q, want npm", got["web"])
	}
	if _, ok := got["pinned"]; ok {
		t.Errorf("pinned should not warn (it sets image:), got %q", got["pinned"])
	}
	if _, ok := got["shell-only"]; ok {
		t.Errorf("shell-only should not warn, got %q", got["shell-only"])
	}
}

func TestParseSelfDependency(t *testing.T) {
	_, err := Parse([]byte(`version: "1"` + "\n" + `jobs: {a: {needs: [a], steps: [{run: x}]}}`))
	if err == nil || !strings.Contains(err.Error(), "itself") {
		t.Fatalf("want self-dependency error, got %v", err)
	}
}
