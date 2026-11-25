package bounded

// Beside runs work on a goroutine of its own and returns at once with a waiter for its value, so a caller can go on
// while work runs and join it where the value is read (#ybjbqm2). With pool set, work runs under one of its slots,
// taken at priority, so the run's CPU spent on it is counted in the run's slots as all its other work is; with pool
// nil it takes none. The waiter returns once work has ended, the same value on every call. A panic in work reaches
// every caller of the waiter, raised again with its own value on that caller's goroutine, as a fan-out's does
// (panicCarrier), rather than ending the process from the goroutine work ran on. A caller holding one of pool's slots
// never waits on the waiter, since work may wait for that slot (see Pool).
func Beside[Value any](pool *Pool, priority int, work func() Value) func() Value {
	var value Value
	var carrier panicCarrier
	ended := make(chan struct{})
	go func() {
		// Deferred first, so it runs last: the slot is given back before any waiter is let go.
		defer close(ended)
		if pool != nil {
			pool.Acquire(priority)
			// Given back whatever work does, since the carrier recovers a panic in it.
			defer pool.Release()
		}
		carrier.run(func() { value = work() })
	}()
	return func() Value {
		<-ended
		carrier.raise()
		return value
	}
}
