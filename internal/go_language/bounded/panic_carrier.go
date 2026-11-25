package bounded

import "sync/atomic"

// panicCarrier carries a panic in a fan-out's work from the worker goroutine it happened on back to the fan-out's
// caller (#z8ym3x5). Nothing up a worker's stack recovers, so a panic there ends the process, where the same panic on
// the caller's goroutine reaches whatever recovers the run, as cohere-go's run does to report it as its problem. Each
// index's work runs under run, which recovers a panic and keeps the first value recovered, so the worker goes on to
// give its slot back and take its next index, and every index runs, since callers rely on that and some write per
// index; once every worker has returned, raise panics again with that value on the caller's goroutine.
type panicCarrier struct {
	first atomic.Pointer[carriedPanic]
}

// carriedPanic is a value recovered from a worker's work, kept as it is, not wrapped, so a caller that recovers a
// value of a particular type still sees that type.
type carriedPanic struct {
	value any
}

// run runs work, recovering a panic in it and keeping the value when it is the first one recovered.
func (carrier *panicCarrier) run(work func()) {
	defer func() {
		if recovered := recover(); recovered != nil {
			// The first to be recovered is kept and any later one dropped, since the caller can raise only one.
			carrier.first.CompareAndSwap(nil, &carriedPanic{value: recovered})
		}
	}()
	work()
}

// raise panics with the first value run recovered, if any, on the calling goroutine. It is called after the join, when
// no worker is left to recover another.
func (carrier *panicCarrier) raise() {
	if carried := carrier.first.Load(); carried != nil {
		// Invariant: a worker's work panicked, which on the caller's goroutine would have reached the caller with the
		// same value, so it does here.
		panic(carried.value)
	}
}
