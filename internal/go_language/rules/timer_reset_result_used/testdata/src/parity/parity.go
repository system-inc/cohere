// Package parity holds branches on the result of (*time.Timer).Reset and the receives that may drain a timer's channel
// in them, where the port and staticcheck's SA1025 still agree finding for finding. The differential test runs both
// over each and compares.
package parity

import "time"

type fakeTimer struct{ C chan time.Time }

func (timer *fakeTimer) Reset(duration time.Duration) bool { return true }

func consume(values ...any) {}

// Found by both: the drain in the branch taken when Reset reports true.
func drainWhenTrue(timer *time.Timer) {
	if timer.Reset(time.Second) {
		<-timer.C
	}
}

// Found by both: the drain in the else branch.
func drainInElse(timer *time.Timer) {
	if timer.Reset(time.Second) {
		consume("stopped")
	} else {
		<-timer.C
	}
}

// Found by both: the drain nested in an inner if of the branch.
func drainNested(timer *time.Timer, condition bool) {
	if !timer.Reset(time.Second) {
		if condition {
			<-timer.C
		}
	}
}

// Found by both: the drain inside a loop in the branch.
func drainInLoop(timer *time.Timer, times int) {
	if !timer.Reset(time.Second) {
		for index := 0; index < times; index++ {
			<-timer.C
		}
	}
}

// Found by both: a drain that keeps its second result.
func drainCommaOk(timer *time.Timer) bool {
	if !timer.Reset(time.Second) {
		_, open := <-timer.C
		return open
	}
	return false
}

// Found by both: the branch drains and returns.
func drainAndReturn(timer *time.Timer) {
	if !timer.Reset(time.Second) {
		<-timer.C
		return
	}
	consume("running")
}

// Found by neither: the result is negated before it is stored, so the if reads the negation, not the call. A negation
// written in the if's own condition is built as swapped branches instead, and is found.
func storedResult(timer *time.Timer) {
	stopped := !timer.Reset(time.Second)
	if stopped {
		<-timer.C
	}
}

// Found by both: a receive from any receive-only channel of time.Time, here time.After's, counts.
func drainAfter(timer *time.Timer) {
	if timer.Reset(time.Second) {
		<-time.After(time.Millisecond)
	}
}

// Found by both: a timer held by value.
func timerValue() {
	var timer time.Timer
	if timer.Reset(time.Second) {
		<-timer.C
	}
}

// Found by both: inside a function literal.
func insideLiteral(timer *time.Timer) func() {
	return func() {
		if !timer.Reset(time.Second) {
			<-timer.C
		}
	}
}

// Found by neither: the branch receives from a bidirectional channel of time.Time, spelled otherwise.
func bidirectionalChannel(timer *time.Timer, channel chan time.Time) {
	if !timer.Reset(time.Second) {
		<-channel
	}
}

// Found by neither: the branch drains in a select, which is no plain receive.
func drainInSelect(timer *time.Timer) {
	if !timer.Reset(time.Second) {
		select {
		case <-timer.C:
		default:
		}
	}
}

// Found by neither: the drain is after the if, where its paths merge.
func drainAfterIf(timer *time.Timer) {
	if timer.Reset(time.Second) {
		consume("x")
	}
	<-timer.C
}

// Found by neither: the drain is in a function the branch calls.
func drainInCallee(timer *time.Timer) {
	if !timer.Reset(time.Second) {
		drain(timer)
	}
}

func drain(timer *time.Timer) {
	<-timer.C
}

// Found by neither: the result is only printed.
func resultPrinted(timer *time.Timer) {
	consume(timer.Reset(time.Second))
	<-timer.C
}

// Found by neither: Reset through a method value is no static call.
func methodValue(timer *time.Timer) {
	reset := timer.Reset
	if !reset(time.Second) {
		<-timer.C
	}
}

// Found by neither: a Reset of another type.
func otherReset(timer *fakeTimer) {
	if !timer.Reset(time.Second) {
		<-timer.C
	}
}

// Found by neither: the branch receives from another channel.
func otherChannel(timer *time.Timer, channel chan int) {
	if !timer.Reset(time.Second) {
		<-channel
	}
}

// Found by neither: the result is combined with another condition after the call.
func combinedAfter(timer *time.Timer, condition bool) {
	if timer.Reset(time.Second) || condition {
		<-timer.C
	}
}
