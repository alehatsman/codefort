package ci

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// knownToolchains are the language/build tools the default CI base image
// deliberately omits (see ci/Dockerfile — scope is isolation, not toolchains).
// A run: step invoking one of these on the default image fails at run time with
// "<tool>: not found"; ToolchainHints surfaces that at authoring time instead.
var knownToolchains = map[string]bool{
	"go": true, "gofmt": true, "node": true, "npm": true, "npx": true,
	"yarn": true, "pnpm": true, "python": true, "python3": true, "pip": true,
	"pip3": true, "cargo": true, "rustc": true, "java": true, "javac": true,
	"mvn": true, "gradle": true, "ruby": true, "bundle": true, "dotnet": true,
	"deno": true, "bun": true, "make": true, "cmake": true, "gcc": true,
	"g++": true, "clang": true, "tsc": true,
}

// ToolchainHint flags a job whose run: steps invoke a toolchain the default CI
// image lacks while the job pins no image: of its own. Tool is the first such
// toolchain found, for the warning message.
type ToolchainHint struct {
	Job  string
	Tool string
}

// ToolchainHints inspects a (validated) pipeline for a common authoring
// footgun: a job that runs a language/build tool (go, npm, …) but relies on the
// default CI image, which is toolchain-free by design — so it only fails once
// running. A job that pins its own image: is assumed to carry what it needs and
// is skipped (we can't probe an arbitrary image, and an explicit choice is
// likely deliberate). Hints come back in job-name order.
func ToolchainHints(p Pipeline) []ToolchainHint {
	var hints []ToolchainHint
	for _, name := range p.JobNames() {
		job := p.Jobs[name]
		if job.Image != "" {
			continue
		}
		if tool := firstToolchainUsed(job); tool != "" {
			hints = append(hints, ToolchainHint{Job: name, Tool: tool})
		}
	}
	return hints
}

// firstToolchainUsed returns the first known-toolchain command word across a
// job's run: steps, or "". It tokenizes on whitespace and matches whole words,
// so `cd web && npm ci` finds "npm" but a quoted "go to it" does not match "go".
func firstToolchainUsed(job Job) string {
	for i := range job.Steps {
		cmd, isRun, err := inspectStep(&job.Steps[i])
		if err != nil || !isRun {
			continue
		}
		for tok := range strings.FieldsSeq(cmd) {
			if knownToolchains[tok] {
				return tok
			}
		}
	}
	return ""
}

// TranslateJob converts a job's steps into a mooncake playbook: a top-level
// YAML *sequence* of steps (mooncake rejects a `tasks:` map). `run: "<cmd>"`
// sugar becomes `- shell: {cmd: "<cmd>"}`; every other step is a raw mooncake
// step passed through untouched. The job must already be valid (see
// Pipeline.Validate); TranslateJob still surfaces step errors defensively.
func TranslateJob(job Job) ([]byte, error) {
	steps := make([]*yaml.Node, 0, len(job.Steps))
	for i := range job.Steps {
		node := &job.Steps[i]
		runCmd, isRun, err := inspectStep(node)
		if err != nil {
			return nil, fmt.Errorf("step %d: %w", i, err)
		}
		if isRun {
			steps = append(steps, shellStep(runCmd))
		} else {
			steps = append(steps, node)
		}
	}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: steps}
	return yaml.Marshal(seq)
}

// MooncakeStep is one translated step ready to hand to `mooncake step
// '<YAML>'`, plus its action (the step's top-level key) and a human Label for
// the step.started event. Label is what the UI shows in the log timeline: the
// command for a `run:` step, the action otherwise — so a reader sees
// `go test ./...`, not a generic `shell`.
type MooncakeStep struct {
	YAML   string
	Action string
	Label  string
}

// MooncakeSteps renders a job's steps individually, applying the same
// translation as TranslateJob (run: sugar -> shell; raw steps pass through).
// The in-process runner executes each step with `mooncake step` so it can
// observe per-step rc/stdout/stderr and synthesize the event stream.
func MooncakeSteps(job Job) ([]MooncakeStep, error) {
	out := make([]MooncakeStep, 0, len(job.Steps))
	for i := range job.Steps {
		node := &job.Steps[i]
		runCmd, isRun, err := inspectStep(node)
		if err != nil {
			return nil, fmt.Errorf("step %d: %w", i, err)
		}
		n := node
		if isRun {
			n = shellStep(runCmd)
		}
		b, err := yaml.Marshal(n)
		if err != nil {
			return nil, err
		}
		action := stepAction(n)
		label := action
		if isRun {
			label = runCmd
		}
		out = append(out, MooncakeStep{YAML: string(b), Action: action, Label: label})
	}
	return out, nil
}

// stepAction returns a step mapping's top-level key (its mooncake action), or
// "" when indeterminate.
func stepAction(node *yaml.Node) string {
	if node.Kind == yaml.DocumentNode && len(node.Content) == 1 {
		node = node.Content[0]
	}
	if node.Kind == yaml.MappingNode && len(node.Content) >= 1 {
		return node.Content[0].Value
	}
	return ""
}

// inspectStep classifies a step node. It returns (cmd, true, nil) for `run:`
// sugar, ("", false, nil) for a raw mooncake step to pass through, and an
// error for a malformed step. Shared by Validate and TranslateJob so both
// agree on what a well-formed step is.
func inspectStep(node *yaml.Node) (runCmd string, isRun bool, err error) {
	// A document-level node wraps its real content; unwrap it.
	if node.Kind == yaml.DocumentNode && len(node.Content) == 1 {
		node = node.Content[0]
	}
	if node.Kind != yaml.MappingNode {
		return "", false, fmt.Errorf("step must be a mapping (got %s)", kindName(node.Kind))
	}
	if len(node.Content) == 0 {
		return "", false, fmt.Errorf("step is empty")
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key := node.Content[i]
		if key.Value != "run" {
			continue
		}
		if len(node.Content) != 2 {
			return "", false, fmt.Errorf("`run` cannot be combined with other keys")
		}
		val := node.Content[i+1]
		if val.Kind != yaml.ScalarNode || val.Value == "" {
			return "", false, fmt.Errorf("`run` must be a non-empty command string")
		}
		return val.Value, true, nil
	}
	return "", false, nil
}

// shellStep builds the node for `{shell: {cmd: "<cmd>"}}` — mooncake's shell
// action, the target of `run:` sugar.
func shellStep(cmd string) *yaml.Node {
	scalar := func(v string) *yaml.Node {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
	}
	shellBody := &yaml.Node{
		Kind:    yaml.MappingNode,
		Tag:     "!!map",
		Content: []*yaml.Node{scalar("cmd"), scalar(cmd)},
	}
	return &yaml.Node{
		Kind:    yaml.MappingNode,
		Tag:     "!!map",
		Content: []*yaml.Node{scalar("shell"), shellBody},
	}
}

func kindName(k yaml.Kind) string {
	switch k {
	case yaml.ScalarNode:
		return "scalar"
	case yaml.SequenceNode:
		return "sequence"
	case yaml.MappingNode:
		return "mapping"
	case yaml.DocumentNode:
		return "document"
	case yaml.AliasNode:
		return "alias"
	default:
		return "unknown"
	}
}
