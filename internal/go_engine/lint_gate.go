package go_engine

import "sync"

// lintBudget is the extra builds' lint gate's budget, one whole build, counted in thousandths of a build so every
// weight is a whole number and the weights given back always sum to the budget exactly.
const lintBudget = 1000

// heavyLintShare is the weight at or above which a build's lint takes the whole budget and lints alone: one half by
// package count, until adamic's census of each build's live peak calibrates it.
const heavyLintShare = lintBudget / 2

// lintStep is a step of an extra build's lint at the gate, which a test's lintSteps hook is told of.
type lintStep uint8

const (
	// lintStepReady is the build's first load ended without a type error, before it asks for its weight.
	lintStepReady lintStep = iota
	// lintStepQueued is the build in line, since the budget did not hold it or another build was in line first.
	lintStepQueued
	// lintStepTaken is the build holding its weight, before its first rules.
	lintStepTaken
	// lintStepGiving is the build's rules ended, every reload's included, before it gives its weight back.
	lintStepGiving
)

// lintWeight is what an extra build's lint takes of the gate's budget, from what its first load checked from source,
// checkedFromSource of the packagesLoaded of its whole graph: that share of the budget, rounded up so a build that
// checked anything weighs something, or the whole budget once the share is at or above heavyLintShare's one half,
// since two near-full builds linting at once under the memory cap drive the collector (#b2t9xmf). A load of no package
// weighs nothing.
func lintWeight(checkedFromSource int, packagesLoaded int) int {
	if packagesLoaded <= 0 {
		return 0
	}
	if checkedFromSource*lintBudget >= heavyLintShare*packagesLoaded {
		return lintBudget
	}
	return (checkedFromSource*lintBudget + packagesLoaded - 1) / packagesLoaded
}

// lintGate is a weighted semaphore that admits in arrival order, which the extra builds' lints share (#b2t9xmf): a
// build's share of its packages checked from source is exact only once its load has ended, and two near-full builds
// linting side by side under the cap cost about 70 user-s and 6 to 8 s of wall at cohere's root, so each build takes
// its weight (lintWeight) between its load and its rules. golang.org/x/sync's semaphore is only an indirect dependency
// of the module, and the go_engine package had no weighted one, so this is its own.
//
// A build is admitted at once only when the budget holds it and nobody is in line, so a light build that arrives after
// a waiting heavy one waits behind it rather than pass it, and a heavy build is never starved.
//
// It never deadlocks: a build waiting in line holds no weight, since it asks once, after its load and before its first
// rules, and a build holding weight never asks for more, its reloads included. No weight is above the budget, so the
// head of the line is admitted once the builds holding weight have given it back, which each does when its rules end.
type lintGate struct {
	lock    sync.Mutex
	free    int
	waiting []*lintWaiter
}

// lintWaiter is one build in line for weight, admitted once admitted is closed.
type lintWaiter struct {
	weight   int
	admitted chan struct{}
}

// newLintGate is a gate with all of budget free.
func newLintGate(budget int) *lintGate {
	return &lintGate{free: budget}
}

// take holds weight once it is granted, and reports whether the build waited for it. queued, when not nil, is told
// once the build is in line, before it waits.
func (gate *lintGate) take(weight int, queued func()) bool {
	gate.lock.Lock()
	if len(gate.waiting) == 0 && gate.free >= weight {
		gate.free -= weight
		gate.lock.Unlock()
		return false
	}
	waiter := &lintWaiter{weight: weight, admitted: make(chan struct{})}
	gate.waiting = append(gate.waiting, waiter)
	gate.lock.Unlock()
	if queued != nil {
		queued()
	}
	<-waiter.admitted
	return true
}

// giveBack frees weight, and admits from the head of the line every build the budget now holds, stopping at the first
// it does not hold, so none passes another.
func (gate *lintGate) giveBack(weight int) {
	gate.lock.Lock()
	defer gate.lock.Unlock()
	gate.free += weight
	for len(gate.waiting) > 0 && gate.free >= gate.waiting[0].weight {
		gate.free -= gate.waiting[0].weight
		close(gate.waiting[0].admitted)
		gate.waiting = gate.waiting[1:]
	}
}
