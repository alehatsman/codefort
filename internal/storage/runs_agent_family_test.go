package storage

import (
	"path/filepath"
	"testing"
)

func TestListRunsAgentFamilyIncludesSpecVerify(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "rf.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := Migrate(d); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	repoID, err := EnsureRepo(d, "alice", "repo")
	if err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}

	n := 7
	for _, r := range []NewRun{
		{Kind: RunKindCI, CommitSHA: "c1", Ref: "main", Event: "push"},
		{Kind: RunKindAgent, IssueNumber: &n, CommitSHA: "c2", Ref: "main", Event: "agent"},
		{Kind: RunKindSpecVerify, SpecPath: "specs/x.md", CommitSHA: "c3", Ref: "main", Event: "spec-verify"},
	} {
		if _, err := EnqueueRun(d, repoID, r); err != nil {
			t.Fatalf("EnqueueRun %s: %v", r.Kind, err)
		}
	}

	kindsOf := func(f RunFilter) []RunKind {
		runs, err := ListRuns(d, repoID, f)
		if err != nil {
			t.Fatalf("ListRuns: %v", err)
		}
		out := make([]RunKind, len(runs))
		for i, r := range runs {
			out[i] = r.Kind
		}
		return out
	}

	// kind=agent is the agent *family*: agent + spec-verify, not ci.
	agentFam := kindsOf(RunFilter{Kind: RunKindAgent})
	if len(agentFam) != 2 {
		t.Fatalf("agent-family runs = %v, want 2 (agent + spec-verify)", agentFam)
	}
	for _, k := range agentFam {
		if k == RunKindCI {
			t.Errorf("ci run leaked into the agent family: %v", agentFam)
		}
	}

	// kind=ci stays exact.
	ci := kindsOf(RunFilter{Kind: RunKindCI})
	if len(ci) != 1 || ci[0] != RunKindCI {
		t.Errorf("ci filter = %v, want [ci]", ci)
	}

	// no kind = everything.
	if all := kindsOf(RunFilter{}); len(all) != 3 {
		t.Errorf("unfiltered = %v, want 3", all)
	}
}
