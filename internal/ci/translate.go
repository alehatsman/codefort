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

// StepMeta is one job step's action and display Label — the same pair
// TranslateJobPlan bakes into that step's provision `name:` field, and what
// the runner uses to synthesize step.started events in execution order
// (see docs/ops-provisioning.md's CI-runner section: provision's --json
// stream reports a step only once it has finished — there is no separate
// "step started" line — so the runner pre-declares each step's start from
// the plan it already built, in the order `apply` guarantees: a job's steps
// run strictly sequentially, so step N finishing means step N+1 is starting).
type StepMeta struct {
	Action string
	Label  string
}

// JobStepMeta returns each step's action/Label pair, in order. Shares
// classifyStep with TranslateJobPlan so the two can't drift — same
// classification, two different projections of it.
func JobStepMeta(job Job) ([]StepMeta, error) {
	out := make([]StepMeta, 0, len(job.Steps))
	for i := range job.Steps {
		rs, err := classifyStep(&job.Steps[i])
		if err != nil {
			return nil, fmt.Errorf("step %d: %w", i, err)
		}
		out = append(out, StepMeta{Action: rs.action, Label: rs.label})
	}
	return out, nil
}

// TranslateJobPlan renders a job's steps as one provision plan document — a
// top-level YAML sequence (provision spec §3: a plan file's root is a
// sequence; a component's is a mapping, which `apply`/`plan` would reject
// here the same way mooncake rejected a `tasks:` map). `run:` sugar and
// mooncake's own raw shell/cmd/assert-http shapes are rewritten to
// provision's native syntax (see docs/ops-provisioning.md's CI-runner
// section for why each rewrite is needed and how); any other raw step — an
// unrecognized top-level key, or a shell/cmd/assert step already written in
// provision's own shape — passes through untouched, the same escape hatch
// the old mooncake translator offered.
func TranslateJobPlan(job Job) ([]byte, error) {
	steps := make([]*yaml.Node, 0, len(job.Steps))
	for i := range job.Steps {
		rs, err := classifyStep(&job.Steps[i])
		if err != nil {
			return nil, fmt.Errorf("step %d: %w", i, err)
		}
		steps = append(steps, rs.node)
	}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: steps}
	return yaml.Marshal(seq)
}

// rewrittenStep is one step's translation result: the provision-shaped node
// ready to marshal, plus the action/label pair JobStepMeta and
// TranslateJobPlan both need.
type rewrittenStep struct {
	node   *yaml.Node
	action string
	label  string
}

// classifyStep renders one mgitci.yml step into a rewrittenStep. Recognizes
// `run:` sugar, raw `shell: {cmd: "..."}`, raw `cmd: {argv: [...]}`, and raw
// `assert: {http: {...}}` — mooncake's own shapes for those, in real or
// historical (test fixture) use in mgitci.yml. Anything else — an
// unrecognized top-level key, or a shell/cmd/assert step already written in
// provision's own shape (a plain-scalar `shell`, a bare-sequence `cmd`, an
// assert.command/.expr) — passes through unchanged: classifyStep only
// rewrites a shape it can positively identify as mooncake's, never guesses.
func classifyStep(node *yaml.Node) (rewrittenStep, error) {
	n := node
	if n.Kind == yaml.DocumentNode && len(n.Content) == 1 {
		n = n.Content[0]
	}
	if n.Kind != yaml.MappingNode || len(n.Content) == 0 {
		return rewrittenStep{}, fmt.Errorf("step must be a non-empty mapping (got %s)", kindName(n.Kind))
	}

	if runCmd, isRun, err := inspectStep(node); err != nil {
		return rewrittenStep{}, err
	} else if isRun {
		return rewrittenStep{node: shellNode(runCmd), action: "shell", label: runCmd}, nil
	}

	key := n.Content[0].Value
	val := n.Content[1]
	switch key {
	case "shell":
		if cmd, ok := mooncakeShellCmd(val); ok {
			return rewrittenStep{node: shellNode(cmd), action: "shell", label: cmd}, nil
		}
	case "cmd":
		if argv, ok := mooncakeCmdArgv(val); ok {
			return rewrittenStep{node: cmdNode(argv), action: "cmd", label: strings.Join(argv, " ")}, nil
		}
	case "assert":
		a, ok, err := mooncakeHTTPAssert(val)
		if err != nil {
			return rewrittenStep{}, err
		}
		if ok {
			return rewrittenStep{node: httpAssertNode(a), action: "assert", label: "assert " + a.URL}, nil
		}
	}
	return rewrittenStep{node: node, action: key, label: key}, nil
}

// mooncakeShellCmd extracts cmd from mooncake's `shell: {cmd: "..."}` shape,
// or reports ok=false for anything else (already provision-shaped, or
// malformed — malformed is Validate's job to catch, not this one's).
func mooncakeShellCmd(val *yaml.Node) (string, bool) {
	if val.Kind != yaml.MappingNode || len(val.Content) != 2 || val.Content[0].Value != "cmd" {
		return "", false
	}
	if val.Content[1].Kind != yaml.ScalarNode {
		return "", false
	}
	return val.Content[1].Value, true
}

// mooncakeCmdArgv extracts argv from mooncake's `cmd: {argv: [...]}` shape.
func mooncakeCmdArgv(val *yaml.Node) ([]string, bool) {
	if val.Kind != yaml.MappingNode || len(val.Content) != 2 || val.Content[0].Value != "argv" {
		return nil, false
	}
	seq := val.Content[1]
	if seq.Kind != yaml.SequenceNode {
		return nil, false
	}
	argv := make([]string, 0, len(seq.Content))
	for _, item := range seq.Content {
		if item.Kind != yaml.ScalarNode {
			return nil, false
		}
		argv = append(argv, item.Value)
	}
	return argv, true
}

// httpAssertSpec is mgitci.yml's mooncake-native assert.http shape.
type httpAssertSpec struct {
	URL      string
	Contains string
}

// mooncakeHTTPAssert extracts url/status/contains from mooncake's
// `assert: {http: {...}}` shape and validates it translates. provision has
// no built-in HTTP prober (spec §6.7: assert takes command/expr only), so
// this becomes a curl-based command assert (httpAssertNode) — see
// docs/ops-provisioning.md's CI-runner section. Only status 200 is
// supported: there is no single curl invocation that checks an arbitrary
// status *and* a body substring at once (curl -f discards the body on a
// non-2xx response), and every http assert in mgitci.yml checks 200
// (audited for #411) — a different status is a translation error, not a
// silent wrong check.
func mooncakeHTTPAssert(val *yaml.Node) (httpAssertSpec, bool, error) {
	if val.Kind != yaml.MappingNode || len(val.Content) != 2 || val.Content[0].Value != "http" {
		return httpAssertSpec{}, false, nil
	}
	var raw struct {
		URL      string `yaml:"url"`
		Status   int    `yaml:"status"`
		Contains string `yaml:"contains"`
	}
	if err := val.Content[1].Decode(&raw); err != nil {
		return httpAssertSpec{}, false, fmt.Errorf("assert.http: %w", err)
	}
	if raw.URL == "" {
		return httpAssertSpec{}, false, fmt.Errorf("assert.http: url is required")
	}
	if raw.Status != 0 && raw.Status != 200 {
		return httpAssertSpec{}, false, fmt.Errorf("assert.http: status %d not supported by the provision translator (only 200 — see docs/ops-provisioning.md)", raw.Status)
	}
	return httpAssertSpec{URL: raw.URL, Contains: raw.Contains}, true, nil
}

// shellNode builds a provision shell step: `{name, shell, changed_when:
// "false"}`. changed_when is required: a bare shell step with no
// idempotency gate reports `changed: unknown` and fails `validate --strict`
// (spec §6.1) — every CI step is exit-code-is-the-contract, not a state
// change (matches #410's own tasks/*.yml idiom).
func shellNode(cmd string) *yaml.Node {
	return mapNode(
		scalar("name"), scalar(cmd),
		scalar("shell"), scalar(cmd),
		scalar("changed_when"), scalar("false"),
	)
}

// cmdNode builds a provision cmd step: `{name, cmd: [...], changed_when:
// "false"}` — same idempotency rule as shellNode.
func cmdNode(argv []string) *yaml.Node {
	items := make([]*yaml.Node, len(argv))
	for i, a := range argv {
		items[i] = scalar(a)
	}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: items}
	return mapNode(
		scalar("name"), scalar(strings.Join(argv, " ")),
		scalar("cmd"), seq,
		scalar("changed_when"), scalar("false"),
	)
}

// httpAssertNode builds a provision command-form assert equivalent to
// mooncake's `assert: {http: {url, status: 200, contains}}` — see
// mooncakeHTTPAssert. assert never changes anything (spec §6.7), so unlike
// shellNode/cmdNode it needs no changed_when.
func httpAssertNode(a httpAssertSpec) *yaml.Node {
	cmd := fmt.Sprintf("curl -sf %s", a.URL)
	msg := fmt.Sprintf("%s did not return 200", a.URL)
	if a.Contains != "" {
		cmd = fmt.Sprintf("curl -sf %s | grep -q %s", a.URL, shellQuote(a.Contains))
		msg = fmt.Sprintf("%s did not return 200 containing %q", a.URL, a.Contains)
	}
	return mapNode(
		scalar("name"), scalar("assert "+a.URL),
		scalar("assert"), mapNode(scalar("command"), scalar(cmd), scalar("msg"), scalar(msg)),
	)
}

// shellQuote wraps s in single quotes for embedding in a generated shell
// command line, escaping any single quote it contains.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func scalar(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}

func mapNode(pairs ...*yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: pairs}
}

// inspectStep classifies a step node. It returns (cmd, true, nil) for `run:`
// sugar, ("", false, nil) for a raw step to pass through, and an error for a
// malformed step. Shared by Validate and classifyStep so both agree on what a
// well-formed step is.
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
