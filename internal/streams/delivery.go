package streams

import "sync"

// delivery is what is known about whether the run's output reached its reader by a route other than the two
// buffered streams, and whether a failure has been said yet.
//
// The other route is the run cache's tee, which copies what the streams write to the real terminal from a
// goroutine of its own, so a failure there is reported here and counted by Finish. The lock is for that
// goroutine: Report runs on it, Finish on the main one.
type delivery struct {
	lock sync.Mutex
	// failure is the first failure reported, nil while every copy arrived.
	failure error
	// said is whether a failure has been written to the last resort, so a second Finish does not say it again.
	said bool
}

// report keeps failure when it is the first.
func (state *delivery) report(failure error) {
	state.lock.Lock()
	defer state.lock.Unlock()
	if state.failure == nil {
		state.failure = failure
	}
}

// settle is whether the run's output failed to reach its reader, by flushFailure or by any failure reported,
// and the failure still to be said: the first reported, or flushFailure when none was, and nil once it has
// been said, so it is said once however many times the process finishes.
func (state *delivery) settle(flushFailure error) (bool, error) {
	state.lock.Lock()
	defer state.lock.Unlock()
	failure := state.failure
	if failure == nil {
		failure = flushFailure
	}
	if failure == nil {
		return false, nil
	}
	if state.said {
		return true, nil
	}
	state.said = true
	return true, failure
}
