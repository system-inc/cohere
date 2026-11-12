package bounded

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
)

// PriorityScale is the top of the scale ScaledPriority puts a graph's priorities on, the priority of the work on its
// longest path.
const PriorityScale = 1 << 20

// nestedCheckVariable, set to anything in a test's environment, has every pool made after panic when a goroutine
// holding one of its slots asks for another (#ckm0hsp): a holder that waits for a slot while it holds one can wait on
// every other holder doing the same, and the pool's freedom from deadlock rests on no holder ever doing so.
const nestedCheckVariable = "COHERE_TEST_POOL_NESTED"

// Pool is a counting semaphore that grants its slots longest path first, the one set of slots a run's every load and
// check shares, the host's and every extra build's, so the run never holds more than size slots at once whatever how
// many builds run side by side (#ckm0hsp). A slot free while nobody waits is taken at once; otherwise the asker waits,
// and each slot given back goes straight to the waiter with the largest priority, of those alike the one that began
// waiting first, so the order is the same for the same arrivals. Granting in arrival order, as a channel does, left the
// critical chain's root parsing near the end of the parse wave, about 150 ms after the graph was ready, and waiting 81
// to 111 ms more for slots between the chain's type checks (#jp0cqkk); across builds it would hold one build's critical
// chain behind another's leaves. Priorities from different graphs are compared on one scale (ScaledPriority).
//
// A holder never asks for a second slot while it holds one, nor waits on work that needs one, so the pool cannot
// deadlock: every holder is running, and gives its slot back.
type Pool struct {
	lock sync.Mutex
	size int

	// free is how many slots nobody holds, never above 0 while anyone waits, since a slot given back then goes to a
	// waiter rather than back to free.
	free int

	// waiting is a binary heap of the waiters, the one to grant next on top (grantsBefore).
	waiting []*poolWaiter

	// arrivals counts every waiter so far, each one's arrival its place in that count, which breaks ties of priority.
	arrivals int

	// held is how many slots are held now, observe, nil outside tests, is told each slot taken, +1, and each given
	// back, -1, so a test can count the slots held across several pools, and holders, nil unless nestedCheckVariable
	// is set, are the goroutines holding a slot.
	held     int
	observe  func(change int)
	holders  map[uint64]int
	checking bool
}

// poolWaiter is one asker waiting for a slot, granted once granted is closed.
type poolWaiter struct {
	priority int
	arrival  int
	granted  chan struct{}
}

// NewPool is a pool of size slots, every one free, at least one.
func NewPool(size int) *Pool {
	pool := &Pool{size: max(size, 1), free: max(size, 1)}
	if os.Getenv(nestedCheckVariable) != "" {
		pool.checking = true
		pool.holders = map[uint64]int{}
	}
	return pool
}

// Size is how many slots the pool has.
func (pool *Pool) Size() int {
	return pool.size
}

// Observe has the pool tell observe of each slot taken, +1, and each given back, -1, for a test that counts the slots
// held across every pool it watches. It is set before the pool is shared.
func (pool *Pool) Observe(observe func(change int)) {
	pool.observe = observe
}

// ScaledPriority is priority, of a graph whose longest path's priority is longest, on PriorityScale, so the work of two
// graphs counted in different units, a load's files or a checker's bytes, waits in one order: the larger share of its
// own graph's longest path first. It is 1 or more for every priority above 0, and 0 for 0, which stays below every
// other.
func ScaledPriority(priority int, longest int) int {
	if priority <= 0 || longest <= 0 {
		return 0
	}
	return 1 + int(int64(min(priority, longest))*(PriorityScale-1)/int64(longest))
}

// Acquire takes a slot for work of priority, waiting until one is granted.
func (pool *Pool) Acquire(priority int) {
	pool.lock.Lock()
	if pool.checking {
		pool.refuseNested()
	}
	if pool.free > 0 {
		pool.free--
		pool.taken()
		pool.lock.Unlock()
		return
	}
	waiter := &poolWaiter{priority: priority, arrival: pool.arrivals, granted: make(chan struct{})}
	pool.arrivals++
	pool.push(waiter)
	pool.lock.Unlock()
	<-waiter.granted
	pool.lock.Lock()
	defer pool.lock.Unlock()
	if pool.checking {
		pool.holders[goroutineIdentifier()]++
	}
	if pool.observe != nil {
		pool.observe(1)
	}
}

// Release gives a slot back: to the waiter granted next when anyone waits, else to the free slots.
func (pool *Pool) Release() {
	pool.lock.Lock()
	defer pool.lock.Unlock()
	if pool.checking {
		identifier := goroutineIdentifier()
		pool.holders[identifier]--
		if pool.holders[identifier] <= 0 {
			delete(pool.holders, identifier)
		}
	}
	if pool.observe != nil {
		pool.observe(-1)
	}
	if len(pool.waiting) == 0 {
		pool.free++
		pool.held--
		return
	}
	// The slot passes straight to the waiter, so the count held stands; the waiter counts as its holder once it runs.
	close(pool.take().granted)
}

// ForEach runs work for every index below count on the pool's slots, each index under a slot taken at priority, on at
// most as many goroutines as the pool has slots, and returns once every one has. Each goroutine takes one slot per
// index and gives it back before the next, so none holds two, and the caller holds none while it waits. work must keep
// what it makes in its own index's place, since the indexes finish in no set order.
func (pool *Pool) ForEach(count int, priority int, work func(index int)) {
	var next atomic.Int64
	var group sync.WaitGroup
	for range min(count, pool.size) {
		group.Go(func() {
			for {
				index := int(next.Add(1) - 1)
				if index >= count {
					return
				}
				pool.Acquire(priority)
				work(index)
				pool.Release()
			}
		})
	}
	group.Wait()
}

// ForEachWriting is ForEach for work that writes to the file system (#g2e4mz8): compute runs for every index under a
// slot taken at priority and returns what is left of that index's work, its writes, which then run with the slot given
// back, so a write blocked in the kernel holds none of the run's cores and other work runs on them. It runs on twice as
// many goroutines as the pool has slots, so while every slot's holder has gone on to write, as many more can compute,
// and no more than that many ever write at once, however slow the disk. Like ForEach it returns once every index's
// writes have, so whatever it wrote is in place when it returns, and no goroutine holds two slots. compute may return
// nil, an index with nothing to write.
func (pool *Pool) ForEachWriting(count int, priority int, compute func(index int) (write func())) {
	var next atomic.Int64
	var group sync.WaitGroup
	for range min(count, 2*pool.size) {
		group.Go(func() {
			for {
				index := int(next.Add(1) - 1)
				if index >= count {
					return
				}
				pool.Acquire(priority)
				write := compute(index)
				pool.Release()
				if write != nil {
					write()
				}
			}
		})
	}
	group.Wait()
}

// Waiters is how many askers wait for a slot now.
func (pool *Pool) Waiters() int {
	pool.lock.Lock()
	defer pool.lock.Unlock()
	return len(pool.waiting)
}

// taken counts a slot taken while nobody waited. The lock is held.
func (pool *Pool) taken() {
	pool.held++
	if pool.checking {
		pool.holders[goroutineIdentifier()]++
	}
	if pool.observe != nil {
		pool.observe(1)
	}
}

// refuseNested panics when the asking goroutine holds a slot already. The lock is held.
func (pool *Pool) refuseNested() {
	if identifier := goroutineIdentifier(); pool.holders[identifier] > 0 {
		pool.lock.Unlock()
		// Invariant: a holder never asks for a second slot (see Pool), which this check, on only in tests, enforces.
		panic(fmt.Sprintf("bounded: goroutine %d asked the pool for a slot while it holds one", identifier))
	}
}

// goroutineIdentifier is the asking goroutine's number, read from its stack's first line, for the nested check alone,
// which runs only in tests: Go names no goroutine otherwise.
func goroutineIdentifier() uint64 {
	buffer := make([]byte, 64)
	buffer = buffer[:runtime.Stack(buffer, false)]
	// The first line reads "goroutine 18 [running]:".
	fields := bytes.Fields(buffer)
	if len(fields) < 2 {
		return 0
	}
	identifier, parseError := strconv.ParseUint(string(fields[1]), 10, 64)
	if parseError != nil {
		return 0
	}
	return identifier
}

// push queues a waiter, moving it up past every parent it is granted before. The lock is held.
func (pool *Pool) push(waiter *poolWaiter) {
	pool.waiting = append(pool.waiting, waiter)
	child := len(pool.waiting) - 1
	for child > 0 {
		parent := (child - 1) / 2
		if !grantsBefore(pool.waiting[child], pool.waiting[parent]) {
			return
		}
		pool.waiting[child], pool.waiting[parent] = pool.waiting[parent], pool.waiting[child]
		child = parent
	}
}

// take removes and returns the waiter on top, which the caller has checked is there, moving the last waiter into its
// place and down past every child granted before it. The lock is held.
func (pool *Pool) take() *poolWaiter {
	top := pool.waiting[0]
	last := len(pool.waiting) - 1
	pool.waiting[0] = pool.waiting[last]
	// The emptied slot lets go of the waiter, so a granted waiter is not held by the heap's backing array.
	pool.waiting[last] = nil
	pool.waiting = pool.waiting[:last]
	parent := 0
	for {
		earliest := parent
		for _, child := range []int{2*parent + 1, 2*parent + 2} {
			if child < len(pool.waiting) && grantsBefore(pool.waiting[child], pool.waiting[earliest]) {
				earliest = child
			}
		}
		if earliest == parent {
			return top
		}
		pool.waiting[parent], pool.waiting[earliest] = pool.waiting[earliest], pool.waiting[parent]
		parent = earliest
	}
}

// grantsBefore reports whether first is to be granted a slot before second: the larger priority, then the earlier
// arrival.
func grantsBefore(first, second *poolWaiter) bool {
	if first.priority != second.priority {
		return first.priority > second.priority
	}
	return first.arrival < second.arrival
}
