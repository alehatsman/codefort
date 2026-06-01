package storage

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

// parkedAgentRun creates an agent run and drives it to awaiting_input (claim ->
// running -> park), the state in which it accepts follow-up turns.
func parkedAgentRun(t *testing.T, db *sql.DB, repoID int64) CIRun {
	t.Helper()
	n := 1
	run, err := EnqueueRun(db, repoID, NewRun{Kind: RunKindAgent, IssueNumber: &n, CommitSHA: "a", Ref: "HEAD", Event: "agent"})
	if err != nil {
		t.Fatalf("EnqueueRun: %v", err)
	}
	if _, err := ClaimNextRunOfKind(db, RunKindAgent, time.Hour); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := MarkRunAwaitingInput(db, run.ID); err != nil {
		t.Fatalf("MarkRunAwaitingInput: %v", err)
	}
	return run
}

func TestCancelAgentRun(t *testing.T) {
	t.Run("cancels a parked (awaiting_input) run", func(t *testing.T) {
		db, repoID := seedRepo(t)
		run := parkedAgentRun(t, db, repoID)
		if err := CancelAgentRun(db, run.ID); err != nil {
			t.Fatalf("CancelAgentRun: %v", err)
		}
		got, _ := GetRun(db, repoID, run.Number)
		if got.Status != RunCanceled || got.FinishedAt == nil {
			t.Errorf("status=%q finished=%v, want canceled + finished_at set", got.Status, got.FinishedAt)
		}
	})

	t.Run("cancels a running run", func(t *testing.T) {
		db, repoID := seedRepo(t)
		n := 1
		run, err := EnqueueRun(db, repoID, NewRun{Kind: RunKindAgent, IssueNumber: &n, CommitSHA: "a", Ref: "HEAD", Event: "agent"})
		if err != nil {
			t.Fatalf("EnqueueRun: %v", err)
		}
		if _, err := ClaimNextRunOfKind(db, RunKindAgent, time.Hour); err != nil { // queued -> running
			t.Fatalf("claim: %v", err)
		}
		if err := CancelAgentRun(db, run.ID); err != nil {
			t.Fatalf("CancelAgentRun: %v", err)
		}
		if got, _ := GetRun(db, repoID, run.Number); got.Status != RunCanceled {
			t.Errorf("status = %q, want canceled", got.Status)
		}
	})

	t.Run("already-terminal run is ErrNotFound", func(t *testing.T) {
		db, repoID := seedRepo(t)
		run := parkedAgentRun(t, db, repoID)
		if err := FinishRun(db, run.ID, RunSuccess); err != nil {
			t.Fatalf("FinishRun: %v", err)
		}
		if err := CancelAgentRun(db, run.ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("CancelAgentRun on terminal run = %v, want ErrNotFound", err)
		}
	})

	t.Run("CI run is not cancelable via this path", func(t *testing.T) {
		db, repoID := seedRepo(t)
		run, err := EnqueueRun(db, repoID, NewRun{Kind: RunKindCI, CommitSHA: "a", Ref: "HEAD", Event: "push"})
		if err != nil {
			t.Fatalf("EnqueueRun: %v", err)
		}
		if err := CancelAgentRun(db, run.ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("CancelAgentRun on CI run = %v, want ErrNotFound (kind guard)", err)
		}
	})
}

func TestEnqueueTurnAllocatesSeq(t *testing.T) {
	db, repoID := seedRepo(t)
	n := 1
	run, err := EnqueueRun(db, repoID, NewRun{Kind: RunKindAgent, IssueNumber: &n, CommitSHA: "a", Ref: "HEAD", Event: "agent"})
	if err != nil {
		t.Fatalf("EnqueueRun: %v", err)
	}
	for want := 1; want <= 3; want++ {
		turn, err := EnqueueTurn(db, run.ID, "alice", "msg")
		if err != nil {
			t.Fatalf("EnqueueTurn: %v", err)
		}
		if turn.Seq != want {
			t.Errorf("turn seq = %d, want %d", turn.Seq, want)
		}
		if turn.Status != TurnPending {
			t.Errorf("turn status = %q, want pending", turn.Status)
		}
	}
	turns, err := ListTurns(db, run.ID)
	if err != nil {
		t.Fatalf("ListTurns: %v", err)
	}
	if len(turns) != 3 {
		t.Errorf("ListTurns len = %d, want 3", len(turns))
	}
}

func TestClaimNextTurnRequiresAwaitingInput(t *testing.T) {
	db, repoID := seedRepo(t)
	n := 1
	run, err := EnqueueRun(db, repoID, NewRun{Kind: RunKindAgent, IssueNumber: &n, CommitSHA: "a", Ref: "HEAD", Event: "agent"})
	if err != nil {
		t.Fatalf("EnqueueRun: %v", err)
	}
	if _, err := EnqueueTurn(db, run.ID, "alice", "do more"); err != nil {
		t.Fatalf("EnqueueTurn: %v", err)
	}

	// The run is still queued (not awaiting_input): no turn is dispatchable.
	if _, _, err := ClaimNextTurn(db, time.Hour); !errors.Is(err, ErrNoTurnPending) {
		t.Fatalf("claim on queued run err = %v, want ErrNoTurnPending", err)
	}

	// Drive it to awaiting_input, then the turn claims and flips it to running.
	if _, err := ClaimNextRunOfKind(db, RunKindAgent, time.Hour); err != nil {
		t.Fatalf("claim run: %v", err)
	}
	if err := MarkRunAwaitingInput(db, run.ID); err != nil {
		t.Fatalf("MarkRunAwaitingInput: %v", err)
	}
	turn, claimedRun, err := ClaimNextTurn(db, time.Hour)
	if err != nil {
		t.Fatalf("ClaimNextTurn: %v", err)
	}
	if turn.Status != TurnRunning {
		t.Errorf("claimed turn status = %q, want running", turn.Status)
	}
	if claimedRun.ID != run.ID || claimedRun.Status != RunRunning {
		t.Errorf("claimed run = %d/%s, want %d/running", claimedRun.ID, claimedRun.Status, run.ID)
	}
	// Run is now running (not awaiting_input) -> nothing more to claim.
	if _, _, err := ClaimNextTurn(db, time.Hour); !errors.Is(err, ErrNoTurnPending) {
		t.Fatalf("second claim err = %v, want ErrNoTurnPending", err)
	}

	if err := FinishTurn(db, turn.ID, TurnDone); err != nil {
		t.Fatalf("FinishTurn: %v", err)
	}
	got, _ := ListTurns(db, run.ID)
	if got[0].Status != TurnDone {
		t.Errorf("finished turn status = %q, want done", got[0].Status)
	}
}

func TestMarkRunAwaitingInputOnlyFromRunning(t *testing.T) {
	db, repoID := seedRepo(t)
	n := 1
	run, err := EnqueueRun(db, repoID, NewRun{Kind: RunKindAgent, IssueNumber: &n, CommitSHA: "a", Ref: "HEAD", Event: "agent"})
	if err != nil {
		t.Fatalf("EnqueueRun: %v", err)
	}
	// queued -> awaiting_input is not allowed (CAS requires running) -> no-op.
	if err := MarkRunAwaitingInput(db, run.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("park from queued err = %v, want ErrNotFound (no-op CAS)", err)
	}
}

func TestFinishingFlow(t *testing.T) {
	db, repoID := seedRepo(t)
	run := parkedAgentRun(t, db, repoID)

	// awaiting_input -> finishing accepted.
	if err := MarkRunFinishing(db, run.ID); err != nil {
		t.Fatalf("MarkRunFinishing: %v", err)
	}
	// The runner claims it for handoff (finishing -> running).
	claimed, err := ClaimNextFinishingRun(db)
	if err != nil {
		t.Fatalf("ClaimNextFinishingRun: %v", err)
	}
	if claimed.ID != run.ID || claimed.Status != RunRunning {
		t.Errorf("claimed = %d/%s, want %d/running", claimed.ID, claimed.Status, run.ID)
	}
	// No second claim.
	if _, err := ClaimNextFinishingRun(db); !errors.Is(err, ErrNoRunQueued) {
		t.Fatalf("second claim err = %v, want ErrNoRunQueued", err)
	}
}

func TestMarkRunFinishingRejectsNonParked(t *testing.T) {
	db, repoID := seedRepo(t)
	n := 1
	run, err := EnqueueRun(db, repoID, NewRun{Kind: RunKindAgent, IssueNumber: &n, CommitSHA: "a", Ref: "HEAD", Event: "agent"})
	if err != nil {
		t.Fatalf("EnqueueRun: %v", err)
	}
	// queued (not awaiting_input) -> finish is a no-op CAS.
	if err := MarkRunFinishing(db, run.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("finish on queued err = %v, want ErrNotFound", err)
	}
}

func TestReconcileFinalizesAwaitingInput(t *testing.T) {
	db, repoID := seedRepo(t)
	run := parkedAgentRun(t, db, repoID)
	if _, err := EnqueueTurn(db, run.ID, "alice", "pending work"); err != nil {
		t.Fatalf("EnqueueTurn: %v", err)
	}

	cnt, err := ReconcileOrphanRuns(db)
	if err != nil {
		t.Fatalf("ReconcileOrphanRuns: %v", err)
	}
	if cnt != 1 {
		t.Errorf("reconciled = %d, want 1 (the awaiting_input agent run)", cnt)
	}
	// An orphaned (restart-stranded) run reconciles to interrupted, not error —
	// the runner went away, it wasn't a gate failure (#143). Its un-finished
	// turns still error (no interrupted turn state).
	after, _ := GetRun(db, repoID, run.Number)
	if after.Status != RunInterrupted {
		t.Errorf("run status = %q, want interrupted", after.Status)
	}
	turns, _ := ListTurns(db, run.ID)
	if turns[0].Status != TurnError {
		t.Errorf("orphaned turn status = %q, want error", turns[0].Status)
	}
}
