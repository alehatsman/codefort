package main

import "testing"

func TestWorkBudgetCapsAndFloor(t *testing.T) {
	// total 4, reserve 2 for agents → CI may take at most 2, agents up to 4.
	b := newWorkBudget(4, 2)

	// CI is capped at total-reserved (2), even though the shared pool has 4.
	if !b.tryAcquireCI() || !b.tryAcquireCI() {
		t.Fatal("first two CI acquires should succeed")
	}
	if b.tryAcquireCI() {
		t.Fatal("third CI acquire should fail — CI capped at total-reserved (2)")
	}

	// The reserved slots remain available to agents.
	if !b.tryAcquireAgent() || !b.tryAcquireAgent() {
		t.Fatal("agents should still get the 2 reserved slots")
	}
	// Now total (4) is full — even agents are capped at total.
	if b.tryAcquireAgent() {
		t.Fatal("agent acquire past total should fail")
	}

	// Releasing a CI slot frees both a shared and a ciAllow token.
	b.releaseCI()
	if !b.tryAcquireAgent() {
		t.Fatal("agent should acquire the freed shared slot")
	}
	if b.tryAcquireCI() {
		t.Fatal("CI still capped: ciAllow has 1 free but shared is full")
	}
}

func TestWorkBudgetNoStarvationOfAgents(t *testing.T) {
	// A CI flood can never take the reserved slots: fill CI to its cap, agents
	// must still be able to start (the #268/#293 guarantee).
	b := newWorkBudget(8, 3) // CI cap 5, 3 reserved for agents
	for i := 0; i < 5; i++ {
		if !b.tryAcquireCI() {
			t.Fatalf("CI acquire %d should succeed (cap 5)", i)
		}
	}
	if b.tryAcquireCI() {
		t.Fatal("CI past its cap (5) should fail")
	}
	for i := 0; i < 3; i++ {
		if !b.tryAcquireAgent() {
			t.Fatalf("agent acquire %d should succeed despite CI flood (reserved 3)", i)
		}
	}
}

func TestWorkBudgetClamps(t *testing.T) {
	// reserved > total clamps to total → CI gets nothing, all for agents.
	b := newWorkBudget(2, 5)
	if b.tryAcquireCI() {
		t.Fatal("with reserved>=total, CI should get no slots")
	}
	if !b.tryAcquireAgent() || !b.tryAcquireAgent() {
		t.Fatal("agents should get all of total")
	}
	if b.tryAcquireAgent() {
		t.Fatal("agent past total should fail")
	}

	// total < 1 clamps to 1.
	one := newWorkBudget(0, 0)
	if !one.tryAcquireCI() {
		t.Fatal("total clamped to 1: one CI acquire should succeed")
	}
	if one.tryAcquireCI() || one.tryAcquireAgent() {
		t.Fatal("only one slot total")
	}
}
