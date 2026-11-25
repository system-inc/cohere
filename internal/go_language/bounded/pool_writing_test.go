package bounded

import (
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// A write holds no slot (#g2e4mz8): with the pool's one slot, the first index's write waits until the second index has
// computed, which needs that slot, so a write that kept its slot would wait forever. The deadline turns that into a
// failure rather than a hung test.
func TestAWriteHoldsNoSlot(test *testing.T) {
	test.Parallel()
	pool := NewPool(1)
	secondComputed := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		pool.ForEachWriting(2, 0, func(index int) func() {
			if index == 1 {
				close(secondComputed)
				return nil
			}
			return func() { <-secondComputed }
		})
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		test.Fatal("the first index's write still waits for the second index's compute after 30s: the write holds " +
			"the pool's only slot")
	}
}

// No more writes run at once than twice the pool's slots, however many indexes there are (#g2e4mz8): with every write
// blocked, the goroutines ForEachWriting started are exactly that many, so a slow disk can't grow them without end.
//
// Not parallel: it counts this process's goroutines, which another test's would move.
func TestWritesAreBoundedByTwiceTheSlots(test *testing.T) {
	const size = 2
	const count = 40
	pool := NewPool(size)
	release := make(chan struct{})
	var writing atomic.Int64
	before := runtime.NumGoroutine()
	done := make(chan struct{})
	go func() {
		defer close(done)
		pool.ForEachWriting(count, 0, func(int) func() {
			return func() {
				writing.Add(1)
				<-release
			}
		})
	}()
	// A write that kept its slot would let only size of them start, so the wait has a deadline.
	deadline := time.Now().Add(30 * time.Second)
	for writing.Load() < 2*size {
		if time.Now().After(deadline) {
			close(release)
			test.Fatalf("%d writes started in 30s, want %d at once: a write holds its slot", writing.Load(), 2*size)
		}
		runtime.Gosched()
	}
	// The goroutine running ForEachWriting itself, and every goroutine it started, each blocked in a write.
	if started := runtime.NumGoroutine() - before - 1; started != 2*size {
		test.Errorf("%d goroutines ran the writes, want %d, twice the pool's slots", started, 2*size)
	}
	close(release)
	<-done
	if got := writing.Load(); got != count {
		test.Errorf("%d writes ran, want every one of %d", got, count)
	}
}

// ForEachWriting returns only once every index's writes have run, so whatever it wrote is in place, and counted, when
// it returns (#g2e4mz8): each write takes a little while here, and every one has ended by the return.
func TestEveryWriteEndsBeforeForEachWritingReturns(test *testing.T) {
	test.Parallel()
	const count = 16
	pool := NewPool(2)
	var ended atomic.Int64
	pool.ForEachWriting(count, 0, func(int) func() {
		return func() {
			time.Sleep(20 * time.Millisecond)
			ended.Add(1)
		}
	})
	if got := ended.Load(); got != count {
		test.Errorf("%d of %d writes had ended when ForEachWriting returned, want every one", got, count)
	}
}
