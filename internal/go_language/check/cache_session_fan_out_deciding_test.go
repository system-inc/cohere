package check

import (
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/go_language/bounded"
)

// TestTheDecideIsGrantedSlotsBeforeAnalysis holds the decide's fan-out to the top of the pool's scale, as a single
// verdict's slot is (#9fer4tx): an extra build's verdicts, waiting beside another build's analysis for the run's slots,
// are granted first, since the build waits on every one of them. At priority 0 they queued behind every action of every
// build at width.
func TestTheDecideIsGrantedSlotsBeforeAnalysis(test *testing.T) {
	test.Parallel()

	pool := bounded.NewPool(1)
	session := &CacheSession{caching: &Caching{Pool: pool}}
	// The one slot is held, so both askers below wait, and the order the slot is given in is the pool's to choose.
	pool.Acquire(bounded.PriorityScale)
	granted := make(chan string, 2)
	analyzing := make(chan struct{})
	go func() {
		// An action halfway along another graph's longest path, which arrives first.
		pool.Acquire(bounded.ScaledPriority(1, 2))
		granted <- "analysis"
		pool.Release()
		close(analyzing)
	}()
	waitForPoolWaiters(test, pool, 1)
	deciding := make(chan struct{})
	go func() {
		session.fanOutDeciding(1, func(int) { granted <- "decide" })
		close(deciding)
	}()
	waitForPoolWaiters(test, pool, 2)
	pool.Release()
	<-deciding
	<-analyzing
	if first := <-granted; first != "decide" {
		test.Fatalf("the slot went to the %s first, want the decide's verdict: the build waits on every one", first)
	}
}

// waitForPoolWaiters waits until count askers wait for one of pool's slots, and fails the test after ten seconds.
func waitForPoolWaiters(test *testing.T, pool *bounded.Pool, count int) {
	test.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for pool.Waiters() < count {
		if time.Now().After(deadline) {
			test.Fatalf("%d askers wait for a slot after ten seconds, want %d", pool.Waiters(), count)
		}
		time.Sleep(time.Millisecond)
	}
}
