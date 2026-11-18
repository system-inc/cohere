package bounded

import (
	"errors"
	"testing"
)

// Work run beside its caller holds one of the pool's slots while it runs and gives it back once it ends, whether it
// returns or panics, and its waiter returns its value, or raises its panic with the sentinel itself, on every call.
func TestWorkBesideHoldsOneSlotAndReachesItsWaiter(test *testing.T) {
	test.Parallel()
	pool := NewPool(2)
	heldWhileRunning := -1
	wait := Beside(pool, 0, func() string {
		pool.lock.Lock()
		heldWhileRunning = pool.held
		pool.lock.Unlock()
		return "collected"
	})
	for range 2 {
		if value := wait(); value != "collected" {
			test.Errorf("the waiter returned %q, want %q", value, "collected")
		}
	}
	if heldWhileRunning != 1 {
		test.Errorf("the work ran with %d of the pool's slots held, want 1", heldWhileRunning)
	}
	expectEverySlotFree(test, pool)

	sentinel := errors.New("bounded: the sentinel panic beside")
	panicking := Beside(pool, 0, func() string { panic(sentinel) })
	for range 2 {
		// Compared by identity, so a wrapped or copied value fails.
		if recovered := recovering(func() { panicking() }); recovered != sentinel {
			test.Errorf("the waiter raised %v, want the sentinel itself", recovered)
		}
	}
	expectEverySlotFree(test, pool)
}
