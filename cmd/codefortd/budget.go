package main

// workBudget is the shared run-concurrency budget (#293): CI and agent-family
// runs draw from one pool of `total` slots, with `reserved` slots that only
// agents may take — so a CI backlog can never lock out an agent spawn (and the
// fair, non-blocking drain keeps the inverse from happening too).
//
// Two channels back it: `shared` (size total) caps everyone; `ciAllow` (size
// total-reserved) caps CI. A CI run holds one of each; an agent run holds only
// a shared slot. Hence total in flight ≤ total, CI ≤ total-reserved, and at
// least `reserved` shared slots are always reachable only by agents.
//
// All acquires are non-blocking (try): the runner's drain loop must never block
// one kind behind another (the #268 starvation), so a full budget just means
// "skip this tick, retry next poll".
type workBudget struct {
	shared  chan struct{}
	ciAllow chan struct{}
}

func newWorkBudget(total, reserved int) *workBudget {
	if total < 1 {
		total = 1
	}
	if reserved < 0 {
		reserved = 0
	}
	if reserved > total {
		reserved = total
	}
	return &workBudget{
		shared:  make(chan struct{}, total),
		ciAllow: make(chan struct{}, total-reserved),
	}
}

// tryAcquireCI takes one CI slot — a ciAllow token *and* a shared token — or
// returns false having taken nothing. ciAllow is taken first so a CI run never
// holds a shared slot while blocked on its own cap.
func (b *workBudget) tryAcquireCI() bool {
	select {
	case b.ciAllow <- struct{}{}:
	default:
		return false
	}
	select {
	case b.shared <- struct{}{}:
		return true
	default:
		<-b.ciAllow // took nothing — hand the ciAllow token back
		return false
	}
}

func (b *workBudget) releaseCI() {
	<-b.shared
	<-b.ciAllow
}

// tryAcquireAgent takes one shared slot for an agent-family run, or returns
// false. Agents may take the `reserved` slots CI cannot, so a CI flood never
// starves a spawn.
func (b *workBudget) tryAcquireAgent() bool {
	select {
	case b.shared <- struct{}{}:
		return true
	default:
		return false
	}
}

func (b *workBudget) releaseAgent() {
	<-b.shared
}
