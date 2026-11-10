package load

import (
	"runtime"
	"slices"
	"sync"
	"testing"
)

// With its one slot held, waiters queued one after another in a known order, their priorities mixed, are granted
// the slot as it is given back by the larger priority first, and those alike in the order they began waiting.
func TestPrioritySlotsGrantTheLargestPriorityFirstThenByArrival(test *testing.T) {
	test.Parallel()
	slots := newPrioritySlots(1)
	slots.acquire(0)
	waiters := []struct {
		name     string
		priority int
	}{{"first", 1}, {"second", 3}, {"third", 2}, {"fourth", 3}, {"fifth", 1}, {"sixth", 5}}
	var grantedLock sync.Mutex
	var granted []string
	var group sync.WaitGroup
	for index, waiter := range waiters {
		group.Go(func() {
			slots.acquire(waiter.priority)
			grantedLock.Lock()
			granted = append(granted, waiter.name)
			grantedLock.Unlock()
			slots.release()
		})
		// Each waiter is queued before the next starts, so their arrivals are in this order.
		for slots.waiters() < index+1 {
			runtime.Gosched()
		}
	}
	slots.release()
	group.Wait()
	want := []string{"sixth", "second", "fourth", "third", "first", "fifth"}
	if !slices.Equal(granted, want) {
		test.Errorf("the slot was granted to %v, want %v", granted, want)
	}
}

// A slot free while nobody waits is taken at once, and each one given back is free again.
func TestPrioritySlotsTakeAFreeSlotAtOnce(test *testing.T) {
	test.Parallel()
	slots := newPrioritySlots(2)
	slots.acquire(0)
	slots.acquire(7)
	slots.release()
	slots.acquire(3)
	slots.release()
	slots.release()
	if slots.free != 2 || slots.waiters() != 0 {
		test.Errorf("after every slot was given back, %d are free and %d wait, want 2 and 0", slots.free,
			slots.waiters())
	}
}
