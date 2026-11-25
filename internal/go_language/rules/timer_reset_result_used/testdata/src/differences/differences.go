// Package differences holds the cases where the port and the original could plausibly disagree, and records no
// difference, since there is none: both read the same intermediate representation. The original reports once per if
// that reads a result, not once per call; it judges a branch by the blocks its first block dominates, which loops,
// gotos and short-circuit conditions shape; and it stops walking a branch at its first drain.
package differences

import "time"

type embedding struct{ *time.Timer }

// Found by both, twice: one result read by two ifs, each with a drain.
func twoIfs(timer *time.Timer) {
	running := timer.Reset(time.Second)
	if running {
		<-timer.C
	}
	if running {
		<-timer.C
	}
}

// Found by both: a short-circuit condition whose second operand is the call, so the if on the call has a branch of
// its own.
func shortCircuitSecond(timer *time.Timer, condition bool) {
	if condition && timer.Reset(time.Second) {
		<-timer.C
	}
}

// Found by both: Reset promoted from an embedded timer, a static call to the same method.
func promoted(value embedding) {
	if !value.Reset(time.Second) {
		<-value.C
	}
}

// Found by both: the branch loops back on itself before draining.
func loopBeforeDrain(timer *time.Timer, times int) {
	if !timer.Reset(time.Second) {
		for index := 0; index < times; index++ {
		}
		<-timer.C
	}
}

// Found by both: the branch jumps forward with a goto before draining.
func gotoBeforeDrain(timer *time.Timer) {
	if !timer.Reset(time.Second) {
		goto drain
	drain:
		<-timer.C
	}
}

// Found by both, once: two drains in one branch.
func twoDrains(timer *time.Timer) {
	if !timer.Reset(time.Second) {
		<-timer.C
		<-timer.C
	}
}

// Found by both, once each: deferred and goroutine literals.
func literals(timer *time.Timer) {
	defer func() {
		if !timer.Reset(time.Second) {
			<-timer.C
		}
	}()
	go func() {
		if !timer.Reset(time.Second) {
			<-timer.C
		}
	}()
}

// Found by neither: the drain is unreachable, after a return in the branch.
func drainAfterReturn(timer *time.Timer) {
	if !timer.Reset(time.Second) {
		return
		<-timer.C
	}
}
