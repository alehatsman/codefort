package storage

import "testing"

// CountActiveRuns counts only non-terminal runs (queued/running and the
// agent-only awaiting_input/finishing). A run drops out of the count the moment
// it reaches a terminal state.
func TestCountActiveRuns(t *testing.T) {
	db, repoID := seedRepo(t)

	if n, err := CountActiveRuns(db, repoID); err != nil || n != 0 {
		t.Fatalf("CountActiveRuns (empty) = (%d, %v), want (0, nil)", n, err)
	}

	// A freshly enqueued run is queued → counts as active.
	run, err := EnqueueRun(db, repoID, NewRun{CommitSHA: "a", Ref: "refs/heads/main", Event: "push"})
	if err != nil {
		t.Fatalf("EnqueueRun: %v", err)
	}
	if n, _ := CountActiveRuns(db, repoID); n != 1 {
		t.Errorf("CountActiveRuns (queued) = %d, want 1", n)
	}

	// Finishing it to a terminal state drops it from the count.
	if err := FinishRun(db, run.ID, RunSuccess); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}
	if n, _ := CountActiveRuns(db, repoID); n != 0 {
		t.Errorf("CountActiveRuns (finished) = %d, want 0", n)
	}

	// An agent run parked awaiting input is non-terminal → active again.
	num := 1
	agent, err := EnqueueRun(db, repoID, NewRun{
		Kind: RunKindAgent, IssueNumber: &num, CommitSHA: "b", Ref: "HEAD", Event: "agent",
	})
	if err != nil {
		t.Fatalf("EnqueueRun agent: %v", err)
	}
	if _, err := ClaimNextRunOfKind(db, RunKindAgent, 0); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := MarkRunAwaitingInput(db, agent.ID); err != nil {
		t.Fatalf("MarkRunAwaitingInput: %v", err)
	}
	if n, _ := CountActiveRuns(db, repoID); n != 1 {
		t.Errorf("CountActiveRuns (awaiting_input) = %d, want 1", n)
	}
}
