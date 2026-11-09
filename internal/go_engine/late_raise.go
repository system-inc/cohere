package go_engine

import (
	"runtime"
	"runtime/metrics"
	"sync/atomic"
)

// lateRaise raises the memory limit once, at the first moment after the load's end with no collection in flight: at
// the load's end itself when none is, or just after the collection in flight then completes. A collection in flight at
// the load's end is the second of the three, which the raise must follow rather than lift: measured, one was in flight
// in 14 of 20 runs, and a raise during it lets that collection run on a higher goal, so the peak grows.
//
// It learns a collection completed from a finalizer, rather than from a goroutine polling `/gc/cycles` in
// runtime/metrics: a poll keeps a goroutine waking for the whole run and lands as late as its period, while a finalizer
// costs nothing between collections and runs just after the one that freed its object. The sentinel is armed before the
// load and armed again after each collection, rather than made at the load's end, because an object allocated while a
// collection is marking is allocated marked: a sentinel made then would survive the collection in flight and tell of
// the next, the one the raise is there to skip. Each sentinel predates the collection that frees it, unless a
// collection starts before the finalizer of the last has armed the next, which only makes the raise one collection
// late.
type lateRaise struct {
	limit          int64
	setMemoryLimit func(int64) int64
	// collecting is whether a collection is marking now, collectorMarking outside a test.
	collecting func() bool
	loadEnded  atomic.Bool
	raised     atomic.Bool
	// armed is whether a sentinel is waiting on a collection, and collections how many collections the sentinels have
	// seen, which a test waits on.
	armed       atomic.Bool
	collections atomic.Int64
}

// lateRaiseSentinel is the object whose finalizer tells the late raise a collection completed. It holds a pointer so it
// is never a block of the tiny allocator, whose finalizers may never run (see runtime.SetFinalizer).
type lateRaiseSentinel struct {
	_ *byte
}

// newLateRaise arms a late raise to limit, set through setMemoryLimit, debug.SetMemoryLimit outside a test.
func newLateRaise(limit int64, setMemoryLimit func(int64) int64, collecting func() bool) *lateRaise {
	raise := &lateRaise{limit: limit, setMemoryLimit: setMemoryLimit, collecting: collecting}
	raise.arm()
	return raise
}

// arm makes a sentinel and lets go of it, so the next collection to mark without it frees it and runs collected.
func (raise *lateRaise) arm() {
	raise.armed.Store(true)
	runtime.SetFinalizer(&lateRaiseSentinel{}, func(*lateRaiseSentinel) { raise.collected() })
}

// collected is a sentinel's finalizer: a collection has completed. After the load's end it raises, and before it the
// raise arms again for the next collection. Once raised nothing is armed again.
func (raise *lateRaise) collected() {
	raise.armed.Store(false)
	raise.collections.Add(1)
	if raise.loadEnded.Load() {
		raise.raise()
		return
	}
	raise.arm()
}

// endLoad is the load's end: the raise lands now when no collection is in flight, and otherwise after the one in
// flight completes (collected). Only the first load's end counts, since a later load (a reload, the wrap's, the
// rename's) comes after it. It does nothing on a nil raise, a run that raises nothing.
//
// The load's end is marked before the collector is read, so a collection completing between the two raises in
// collected and this call's raise finds it done.
func (raise *lateRaise) endLoad() {
	if raise == nil || raise.loadEnded.Swap(true) {
		return
	}
	if !raise.collecting() {
		raise.raise()
	}
}

// raise sets the late limit, once whoever calls it first.
func (raise *lateRaise) raise() {
	if raise.raised.CompareAndSwap(false, true) {
		raise.setMemoryLimit(raise.limit)
	}
}

// collectorMarking is whether a collection is marking now. Each collection stops the world twice, at its start (sweep
// termination) and at its end (mark termination), and the runtime counts both in `/sched/pauses/total/gc:seconds` as
// the world restarts, so an odd count is a collection between the two. A runtime without the metric reads as one in
// flight, which only waits for a collection to complete before raising.
func collectorMarking() bool {
	samples := []metrics.Sample{{Name: "/sched/pauses/total/gc:seconds"}}
	metrics.Read(samples)
	if samples[0].Value.Kind() != metrics.KindFloat64Histogram {
		return true
	}
	var pauses uint64
	for _, count := range samples[0].Value.Float64Histogram().Counts {
		pauses += count
	}
	return pauses%2 == 1
}
