package storage

import (
	"errors"
	"testing"
	"time"
)

// EnqueueRun with no kind set yields a CI run (the historical default) and no
// issue link, so every pre-agent caller keeps working unchanged.
func TestEnqueueRunDefaultsKindCI(t *testing.T) {
	db, repoID := seedRepo(t)
	run, err := EnqueueRun(db, repoID, NewRun{CommitSHA: "abc", Ref: "refs/heads/main", Event: "push"})
	if err != nil {
		t.Fatalf("EnqueueRun: %v", err)
	}
	if run.Kind != RunKindCI {
		t.Errorf("kind = %q, want %q", run.Kind, RunKindCI)
	}
	if run.IssueNumber != nil {
		t.Errorf("issue_number = %v, want nil", *run.IssueNumber)
	}
	// Survives a round-trip through the DB.
	got, err := GetRun(db, repoID, run.Number)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Kind != RunKindCI || got.IssueNumber != nil {
		t.Errorf("round-trip = (%q, %v), want (ci, nil)", got.Kind, got.IssueNumber)
	}
}

// An agent run records its kind and the issue it serves.
func TestEnqueueAgentRunLinksIssue(t *testing.T) {
	db, repoID := seedRepo(t)
	n := 42
	run, err := EnqueueRun(db, repoID, NewRun{
		Kind: RunKindAgent, IssueNumber: &n, CommitSHA: "abc", Ref: "HEAD", Event: "agent",
	})
	if err != nil {
		t.Fatalf("EnqueueRun: %v", err)
	}
	got, err := GetRun(db, repoID, run.Number)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Kind != RunKindAgent {
		t.Errorf("kind = %q, want %q", got.Kind, RunKindAgent)
	}
	if got.IssueNumber == nil || *got.IssueNumber != 42 {
		t.Errorf("issue_number = %v, want 42", got.IssueNumber)
	}
}

// Claiming is scoped to a kind: the CI pool never picks up an agent run and
// vice versa, so a flood of one kind can't starve the other.
func TestClaimNextRunIsolatesKinds(t *testing.T) {
	db, repoID := seedRepo(t)
	ciRun, err := EnqueueRun(db, repoID, NewRun{CommitSHA: "a", Ref: "refs/heads/main", Event: "push"})
	if err != nil {
		t.Fatalf("EnqueueRun ci: %v", err)
	}
	n := 7
	agentRun, err := EnqueueRun(db, repoID, NewRun{
		Kind: RunKindAgent, IssueNumber: &n, CommitSHA: "b", Ref: "HEAD", Event: "agent",
	})
	if err != nil {
		t.Fatalf("EnqueueRun agent: %v", err)
	}

	// The CI pool claims only the CI run, even though the agent run is older in
	// neither sense — both are queued; kind is the discriminator.
	got, err := ClaimNextRun(db, time.Hour)
	if err != nil {
		t.Fatalf("ClaimNextRun: %v", err)
	}
	if got.Number != ciRun.Number || got.Kind != RunKindCI {
		t.Fatalf("ci claim = #%d/%s, want #%d/ci", got.Number, got.Kind, ciRun.Number)
	}
	if _, err := ClaimNextRun(db, time.Hour); !errors.Is(err, ErrNoRunQueued) {
		t.Fatalf("second ci claim err = %v, want ErrNoRunQueued (agent run must not leak into ci pool)", err)
	}

	// The agent pool claims the agent run, carrying its issue link.
	ga, err := ClaimNextRunOfKind(db, RunKindAgent, time.Hour)
	if err != nil {
		t.Fatalf("ClaimNextRunOfKind agent: %v", err)
	}
	if ga.Number != agentRun.Number || ga.Kind != RunKindAgent {
		t.Fatalf("agent claim = #%d/%s, want #%d/agent", ga.Number, ga.Kind, agentRun.Number)
	}
	if ga.IssueNumber == nil || *ga.IssueNumber != 7 {
		t.Errorf("claimed agent issue = %v, want 7", ga.IssueNumber)
	}
	if _, err := ClaimNextRunOfKind(db, RunKindAgent, time.Hour); !errors.Is(err, ErrNoRunQueued) {
		t.Fatalf("second agent claim err = %v, want ErrNoRunQueued", err)
	}
}
