package load

import "sync"

// slotWork is the kind of work a load's slot is granted for, which the test hook is told (see loader.observeGrant).
type slotWork uint8

const (
	slotWorkRead slotWork = iota
	slotWorkParse
	slotWorkCheck
	slotWorkExportData
)

// prioritySlots is a counting semaphore that grants its slots longest path first. A slot free while nobody waits is
// taken at once; otherwise the asker waits, and each slot given back goes straight to the waiter with the largest
// priority, of those alike the one that began waiting first, so the order is the same for the same arrivals. Granting
// in arrival order, as a channel does, left the critical chain's root parsing near the end of the parse wave, about
// 150 ms after the graph was ready, and waiting 81 to 111 ms more for slots between the chain's type checks (#jp0cqkk).
type prioritySlots struct {
	lock sync.Mutex

	// free is how many slots nobody holds, never above 0 while anyone waits, since a slot given back then goes to a
	// waiter rather than back to free.
	free int

	// waiting is a binary heap of the waiters, the one to grant next on top (grantsBefore).
	waiting []*slotWaiter

	// arrivals counts every waiter so far, each one's arrival its place in that count, which breaks ties of priority.
	arrivals int
}

// slotWaiter is one asker waiting for a slot, granted once granted is closed.
type slotWaiter struct {
	priority int
	arrival  int
	granted  chan struct{}
}

// newPrioritySlots is a semaphore of count slots, every one free.
func newPrioritySlots(count int) *prioritySlots {
	return &prioritySlots{free: count}
}

// acquire takes a slot for work of priority, waiting until one is granted.
func (slots *prioritySlots) acquire(priority int) {
	slots.lock.Lock()
	if slots.free > 0 {
		slots.free--
		slots.lock.Unlock()
		return
	}
	waiter := &slotWaiter{priority: priority, arrival: slots.arrivals, granted: make(chan struct{})}
	slots.arrivals++
	slots.push(waiter)
	slots.lock.Unlock()
	<-waiter.granted
}

// release gives a slot back: to the waiter granted next when anyone waits, else to the free slots.
func (slots *prioritySlots) release() {
	slots.lock.Lock()
	defer slots.lock.Unlock()
	if len(slots.waiting) == 0 {
		slots.free++
		return
	}
	close(slots.take().granted)
}

// waiters is how many askers wait for a slot now.
func (slots *prioritySlots) waiters() int {
	slots.lock.Lock()
	defer slots.lock.Unlock()
	return len(slots.waiting)
}

// push queues a waiter, moving it up past every parent it is granted before. The lock is held.
func (slots *prioritySlots) push(waiter *slotWaiter) {
	slots.waiting = append(slots.waiting, waiter)
	child := len(slots.waiting) - 1
	for child > 0 {
		parent := (child - 1) / 2
		if !grantsBefore(slots.waiting[child], slots.waiting[parent]) {
			return
		}
		slots.waiting[child], slots.waiting[parent] = slots.waiting[parent], slots.waiting[child]
		child = parent
	}
}

// take removes and returns the waiter on top, which the caller has checked is there, moving the last waiter into its
// place and down past every child granted before it. The lock is held.
func (slots *prioritySlots) take() *slotWaiter {
	top := slots.waiting[0]
	last := len(slots.waiting) - 1
	slots.waiting[0] = slots.waiting[last]
	// The emptied slot lets go of the waiter, so a granted waiter is not held by the heap's backing array.
	slots.waiting[last] = nil
	slots.waiting = slots.waiting[:last]
	parent := 0
	for {
		earliest := parent
		for _, child := range []int{2*parent + 1, 2*parent + 2} {
			if child < len(slots.waiting) && grantsBefore(slots.waiting[child], slots.waiting[earliest]) {
				earliest = child
			}
		}
		if earliest == parent {
			return top
		}
		slots.waiting[parent], slots.waiting[earliest] = slots.waiting[earliest], slots.waiting[parent]
		parent = earliest
	}
}

// grantsBefore reports whether first is to be granted a slot before second: the larger priority, then the earlier
// arrival.
func grantsBefore(first, second *slotWaiter) bool {
	if first.priority != second.priority {
		return first.priority > second.priority
	}
	return first.arrival < second.arrival
}
