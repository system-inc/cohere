package bounded

import (
	"runtime"
	"slices"
	"sync"
	"testing"
)

// With its one slot held, waiters queued one after another in a known order, their priorities mixed, are granted
// the slot as it is given back by the larger priority first, and those alike in the order they began waiting.
func TestAPoolGrantsTheLargestPriorityFirstThenByArrival(test *testing.T) {
	test.Parallel()
	pool := NewPool(1)
	pool.Acquire(0)
	waiters := []struct {
		name     string
		priority int
	}{{"first", 1}, {"second", 3}, {"third", 2}, {"fourth", 3}, {"fifth", 1}, {"sixth", 5}}
	var grantedLock sync.Mutex
	var granted []string
	var group sync.WaitGroup
	for index, waiter := range waiters {
		group.Go(func() {
			pool.Acquire(waiter.priority)
			grantedLock.Lock()
			granted = append(granted, waiter.name)
			grantedLock.Unlock()
			pool.Release()
		})
		// Each waiter is queued before the next starts, so their arrivals are in this order.
		for pool.Waiters() < index+1 {
			runtime.Gosched()
		}
	}
	pool.Release()
	group.Wait()
	want := []string{"sixth", "second", "fourth", "third", "first", "fifth"}
	if !slices.Equal(granted, want) {
		test.Errorf("the slot was granted to %v, want %v", granted, want)
	}
}

// A slot free while nobody waits is taken at once, and each one given back is free again.
//
// Not parallel: it takes two slots on one goroutine, which the nested check refuses, so it turns the check off in the
// process's environment for its own pool.
func TestAPoolTakesAFreeSlotAtOnce(test *testing.T) {
	test.Setenv(nestedCheckVariable, "")
	pool := NewPool(2)
	pool.Acquire(0)
	pool.Acquire(7)
	pool.Release()
	pool.Acquire(3)
	pool.Release()
	pool.Release()
	if pool.free != 2 || pool.Waiters() != 0 || pool.held != 0 {
		test.Errorf("after every slot was given back, %d are free, %d held and %d wait, want 2, 0 and 0", pool.free,
			pool.held, pool.Waiters())
	}
}

// Two graphs' priorities counted in different units wait in one order on the pool's scale: each graph's longest path
// at the top, a priority half its graph's longest at half, and 0 below every other.
func TestScaledPrioritiesCompareAcrossGraphs(test *testing.T) {
	test.Parallel()
	files, bytes := ScaledPriority(40, 40), ScaledPriority(9_000_000, 9_000_000)
	if files != bytes || files != PriorityScale {
		test.Errorf("two longest paths scale to %d and %d, want both %d", files, bytes, PriorityScale)
	}
	if half, quarter := ScaledPriority(20, 40), ScaledPriority(2_250_000, 9_000_000); half <= quarter {
		test.Errorf("half of one graph's longest path scales to %d, a quarter of another's to %d, want the half first",
			half, quarter)
	}
	if ScaledPriority(0, 40) != 0 || ScaledPriority(1, 1_000_000_000) < 1 {
		test.Errorf("0 scales to %d and the least priority above it to %d, want 0 and at least 1",
			ScaledPriority(0, 40), ScaledPriority(1, 1_000_000_000))
	}
}

// With the nested check on, as a test's environment sets it, a goroutine that asks for a slot while it holds one
// panics, and one that asks after it gave its slot back does not.
//
// Not parallel: it sets the process's environment, which every pool made meanwhile would read.
func TestAPoolRefusesANestedAcquireUnderTheCheck(test *testing.T) {
	test.Setenv(nestedCheckVariable, "1")
	pool := NewPool(2)
	pool.Acquire(1)
	pool.Release()
	pool.Acquire(1)
	defer func() {
		if recovered := recover(); recovered == nil {
			test.Error("a holder asked for a second slot and the pool granted it, want a panic")
		}
	}()
	pool.Acquire(1)
}
