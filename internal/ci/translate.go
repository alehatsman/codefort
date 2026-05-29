package ci

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

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
