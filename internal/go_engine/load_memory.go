package go_engine

import "sync"

// loadMemory is the memory limit across a run's loads (#44n0vap). Every load runs under the cap, and once no load is
// in flight a late raise may lift the limit at most once, until the next load starts: the first load in flight retires
// the last raise, so none of its sentinels can lift the limit during this load, sets the limit back to the cap, and
// arms a raise of its own, and the last load to end tells that raise its end. The engine's loads are sequential today,
// so the count is 0 or 1, and it is a count so that two loads in flight at once would share one cap and one raise.
//
// It is the one place that knows the raises the run made (raisedTo), for a record written at the run's end.
type loadMemory struct {
	// capBytes is the limit Main decided, the cap, which every load runs under, and limit the late limit.
	capBytes       int64
	limit          int64
	setMemoryLimit func(int64) int64
	collecting     func() bool

	lock sync.Mutex
	// inFlight is how many loads have begun and not ended, current the raise armed by the first of them, and raises
	// every raise the run armed, in order.
	inFlight int
	current  *lateRaise
	raises   []*lateRaise
}

// newLoadMemory is the run's memory across its loads, or nil when lateLimit says the run raises nothing, and in a test
// that decided nothing (memory nil), which leaves the limit alone.
func newLoadMemory(memory *engineMemory, singleThreaded bool) *loadMemory {
	if memory == nil {
		return nil
	}
	limit, raises := lateLimit(*memory, singleThreaded)
	if !raises {
		return nil
	}
	return &loadMemory{
		capBytes:       memory.record.LimitBytes,
		limit:          limit,
		setMemoryLimit: memory.setMemoryLimit,
		collecting:     memory.collecting,
	}
}

// begin is a load's start. The first load in flight retires the last raise before anything else, then sets the limit
// back to the cap, since that raise may have lifted it, and arms this load's raise before the load allocates.
func (memory *loadMemory) begin() {
	if memory == nil {
		return
	}
	memory.lock.Lock()
	defer memory.lock.Unlock()
	memory.inFlight++
	if memory.inFlight > 1 {
		return
	}
	memory.current.retire()
	memory.setMemoryLimit(memory.capBytes)
	memory.current = newLateRaise(memory.limit, memory.setMemoryLimit, memory.collecting)
	memory.raises = append(memory.raises, memory.current)
}

// end is a load's end. The last load in flight to end tells the raise, which lands now or after the collection in
// flight (lateRaise.endLoad).
func (memory *loadMemory) end() {
	if memory == nil {
		return
	}
	memory.lock.Lock()
	defer memory.lock.Unlock()
	memory.inFlight--
	if memory.inFlight > 0 {
		return
	}
	memory.current.endLoad()
}

// raisedTo is every limit the run's raises set, in order, one per raise that landed, and none for a run that raises
// nothing. A raise still waiting on its collection is not in it, nor is one a later load retired before it landed.
func (memory *loadMemory) raisedTo() []int64 {
	if memory == nil {
		return nil
	}
	memory.lock.Lock()
	defer memory.lock.Unlock()
	var limits []int64
	for _, raise := range memory.raises {
		if limit, raised := raise.raisedLimit(); raised {
			limits = append(limits, limit)
		}
	}
	return limits
}
