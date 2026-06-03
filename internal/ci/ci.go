// Package ci parses and validates moongit's per-repo CI spec (mgitci.yml) and
// translates each job into a mooncake playbook so moongitd can execute it.
//
// mgitci.yml describes a pipeline of jobs wired into a DAG by `needs`; moongit
// owns that DAG. mooncake itself is flat — it executes a top-level *list of
// steps* (a `tasks:` map is rejected). So translation is per-job: one job's
// steps become one mooncake step-list playbook. `run: "<cmd>"` is sugar for a
// shell step; any other step is a raw mooncake step passed through untouched.
package ci

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// SchemaVersion is the only mgitci.yml version this parser accepts.
const SchemaVersion = "1"

// Pipeline is a parsed mgitci.yml.
type Pipeline struct {
	Version string         `yaml:"version"`
	On      On             `yaml:"on"`
	Jobs    map[string]Job `yaml:"jobs"`
}

// On declares which git events trigger the pipeline. Only push is supported.
type On struct {
	Push Push `yaml:"push"`
}

// Push filters which branches trigger a run. An empty Branches matches all.
type Push struct {
	Branches []string `yaml:"branches"`
}

// Job is one unit in the pipeline DAG: a list of steps plus the jobs it
// depends on. Needs builds the DAG moongit's runner topo-orders; an empty
// Needs means the job has no dependencies and may run first / in parallel.
type Job struct {
	Needs []string `yaml:"needs"`
	// Image overrides the server default CI image for this job (honored only
	// under docker isolation). It must be glibc-based and carry `mooncake` on
	// PATH — the runner execs `mooncake step` inside it. Empty means "use the
	// server default image".
	Image string `yaml:"image"`
	// DockerSocket mounts /var/run/docker.sock from the host into the job's
	// container, giving steps access to the host Docker daemon. Use for jobs
	// that build or manage container images (DinD-lite). No effect under host
	// isolation (MOONGIT_CI_ISOLATION=none).
	DockerSocket bool `yaml:"docker_socket"`
	// Steps stay as raw YAML nodes so raw mooncake steps survive translation
	// untouched and `run:` sugar can be rewritten precisely.
	Steps []yaml.Node `yaml:"steps"`
}

// Parse decodes and validates an mgitci.yml document. Unknown top-level and
// per-job keys are rejected so authoring typos (e.g. `step:` for `steps:`)
// fail loudly rather than silently doing nothing.
func Parse(data []byte) (Pipeline, error) {
	var p Pipeline
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil {
		return Pipeline{}, fmt.Errorf("parse mgitci.yml: %w", err)
	}
	if err := p.Validate(); err != nil {
		return Pipeline{}, err
	}
	return p, nil
}

// Validate checks the pipeline is well-formed: a known version, at least one
// job, every job has steps, every step is well-formed, and `needs` references
// only existing jobs without forming a cycle.
func (p Pipeline) Validate() error {
	if p.Version != SchemaVersion {
		return fmt.Errorf("version: want %q, got %q", SchemaVersion, p.Version)
	}
	if len(p.Jobs) == 0 {
		return fmt.Errorf("jobs: at least one job is required")
	}

	for _, name := range p.JobNames() {
		job := p.Jobs[name]
		if len(job.Steps) == 0 {
			return fmt.Errorf("job %q: at least one step is required", name)
		}
		for i := range job.Steps {
			if _, _, err := inspectStep(&job.Steps[i]); err != nil {
				return fmt.Errorf("job %q: step %d: %w", name, i, err)
			}
		}
		for _, dep := range job.Needs {
			if _, ok := p.Jobs[dep]; !ok {
				return fmt.Errorf("job %q: needs unknown job %q", name, dep)
			}
			if dep == name {
				return fmt.Errorf("job %q: cannot depend on itself", name)
			}
		}
	}

	if cycle := p.findCycle(); cycle != nil {
		return fmt.Errorf("jobs: dependency cycle: %s", strings.Join(cycle, " -> "))
	}
	return nil
}

// JobNames returns the job names sorted, so validation and translation iterate
// the map (whose order is random) deterministically.
func (p Pipeline) JobNames() []string {
	names := make([]string, 0, len(p.Jobs))
	for name := range p.Jobs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// TopoOrder returns job names in dependency order: a job appears only after
// every job in its `needs`. Ties are broken by sorted name so the order is
// deterministic. The pipeline must be acyclic (Parse guarantees this); a cycle
// yields an error defensively. The runner executes jobs in this order.
func (p Pipeline) TopoOrder() ([]string, error) {
	// Kahn's algorithm over the needs edges, processing ready jobs in sorted
	// name order for determinism.
	indegree := make(map[string]int, len(p.Jobs))
	for _, name := range p.JobNames() {
		indegree[name] = len(p.Jobs[name].Needs)
	}
	order := make([]string, 0, len(p.Jobs))
	for len(order) < len(p.Jobs) {
		progressed := false
		for _, name := range p.JobNames() {
			if indegree[name] != 0 {
				continue
			}
			order = append(order, name)
			indegree[name] = -1 // mark emitted
			// Decrement dependents that need this job.
			for _, other := range p.JobNames() {
				for _, dep := range p.Jobs[other].Needs {
					if dep == name {
						indegree[other]--
					}
				}
			}
			progressed = true
		}
		if !progressed {
			return nil, fmt.Errorf("jobs: dependency cycle prevents ordering")
		}
	}
	return order, nil
}

// findCycle returns a job sequence forming a `needs` cycle, or nil if the DAG
// is acyclic. moongit's runner relies on topo-ordering, so a cycle is fatal.
func (p Pipeline) findCycle() []string {
	const (
		white = 0 // unvisited
		gray  = 1 // on the current DFS stack
		black = 2 // fully explored
	)
	color := make(map[string]int, len(p.Jobs))
	var stack []string

	var visit func(name string) []string
	visit = func(name string) []string {
		color[name] = gray
		stack = append(stack, name)
		for _, dep := range p.Jobs[name].Needs {
			switch color[dep] {
			case gray:
				// Back-edge: slice the stack from the first sighting of dep.
				for i, n := range stack {
					if n == dep {
						return append(append([]string{}, stack[i:]...), dep)
					}
				}
			case white:
				if c := visit(dep); c != nil {
					return c
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[name] = black
		return nil
	}

	for _, name := range p.JobNames() {
		if color[name] == white {
			if c := visit(name); c != nil {
				return c
			}
		}
	}
	return nil
}
