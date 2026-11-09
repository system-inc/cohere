package go_engine

import (
	"runtime"
	"runtime/debug"
	"slices"
	"sync"
	"testing"
	"time"
)

// limitSetter is a fake debug.SetMemoryLimit that keeps every limit set, which a sentinel's finalizer calls from the
// finalizer goroutine.
type limitSetter struct {
	lock sync.Mutex
	set  []int64
}

func (setter *limitSetter) setMemoryLimit(limit int64) int64 {
	setter.lock.Lock()
	defer setter.lock.Unlock()
	setter.set = append(setter.set, limit)
	return engineMemoryCap
}

func (setter *limitSetter) limits() []int64 {
	setter.lock.Lock()
	defer setter.lock.Unlock()
	return slices.Clone(setter.set)
}

// waitFor waits until done holds, failing after 10 seconds: a finalizer runs on the finalizer goroutine some time after
// runtime.GC returns, and nothing says when.
func waitFor(test *testing.T, what string, done func() bool) {
	test.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			test.Fatalf("waited 10s for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// Not parallel: it turns the collector's automatic collections off for the whole process and counts collections, so
// only its own runtime.GC calls may complete one, which a test running beside it would not leave alone.
func TestLateRaise(test *testing.T) {
	previousPercent := debug.SetGCPercent(-1)
	test.Cleanup(func() { debug.SetGCPercent(previousPercent) })
	const limit = engineMemoryCap + lateMemoryAllowance
	notCollecting := func() bool { return false }

	// Not parallel: it counts collections, as its parent does.
	test.Run("before the load's end a collection never raises", func(test *testing.T) {
		setter := &limitSetter{}
		raise := newLateRaise(limit, setter.setMemoryLimit, notCollecting)

		for completed := int64(1); completed <= 2; completed++ {
			runtime.GC()
			waitFor(test, "the sentinel's finalizer", func() bool { return raise.collections.Load() >= completed })

			if set := setter.limits(); len(set) > 0 {
				test.Fatalf("set the limit to %v after collection %d, before the load's end, want nothing", set,
					completed)
			}
			if !raise.armed.Load() {
				test.Fatalf("the raise stopped arming after collection %d, before the load's end, so no collection "+
					"after it would raise", completed)
			}
		}
	})

	// Not parallel: it counts collections, as its parent does.
	test.Run("with no collection in flight the load's end raises at once, and only once", func(test *testing.T) {
		setter := &limitSetter{}
		raise := newLateRaise(limit, setter.setMemoryLimit, notCollecting)

		raise.endLoad()

		if set := setter.limits(); !slices.Equal(set, []int64{limit}) {
			test.Fatalf("set the limit to %v at the load's end, want %v", set, []int64{limit})
		}
		runtime.GC()
		waitFor(test, "the sentinel's finalizer", func() bool { return raise.collections.Load() >= 1 })
		if set := setter.limits(); !slices.Equal(set, []int64{limit}) {
			test.Errorf("set the limit to %v after a collection, want %v once", set, []int64{limit})
		}
		if raise.armed.Load() {
			test.Error("the raise armed again after it raised")
		}
	})

	// Not parallel: it counts collections, as its parent does. The second collection ending just before the load does
	// (#44n0vap's r13 and r14): the first collection to complete after the load's end is then the last one, which the
	// raise must come before.
	test.Run("a collection that completed just before the load's end raises at it, before the next", func(
		test *testing.T,
	) {
		setter := &limitSetter{}
		raise := newLateRaise(limit, setter.setMemoryLimit, collectorMarking)
		runtime.GC()
		waitFor(test, "the sentinel's finalizer", func() bool { return raise.collections.Load() >= 1 })

		raise.endLoad()

		if set := setter.limits(); !slices.Equal(set, []int64{limit}) {
			test.Fatalf("set the limit to %v at the load's end, want %v before the next collection", set,
				[]int64{limit})
		}
		if completed := raise.collections.Load(); completed != 1 {
			test.Errorf("the raise saw %d collections, want the one before the load's end", completed)
		}
	})

	// Not parallel: it counts collections, as its parent does.
	test.Run("a collection in flight at the load's end raises once it completes", func(test *testing.T) {
		setter := &limitSetter{}
		raise := newLateRaise(limit, setter.setMemoryLimit, func() bool { return true })

		raise.endLoad()

		if set := setter.limits(); len(set) > 0 {
			test.Fatalf("set the limit to %v with a collection in flight, want nothing until it completes", set)
		}
		runtime.GC()
		waitFor(test, "the raise", func() bool { return len(setter.limits()) > 0 })
		if set := setter.limits(); !slices.Equal(set, []int64{limit}) {
			test.Errorf("set the limit to %v after the collection, want %v", set, []int64{limit})
		}
		if raise.armed.Load() {
			test.Error("the raise armed again after it raised")
		}
	})

	// Not parallel: it counts collections, as its parent does. A load's raise still waiting on its collection when the
	// next load starts is retired, so that collection completing during the next load lifts nothing.
	test.Run("a retired raise does nothing when its collection completes", func(test *testing.T) {
		setter := &limitSetter{}
		raise := newLateRaise(limit, setter.setMemoryLimit, func() bool { return true })
		raise.endLoad()

		raise.retire()
		runtime.GC()
		waitFor(test, "the sentinel's finalizer", func() bool { return raise.collections.Load() >= 1 })

		if set := setter.limits(); len(set) > 0 {
			test.Errorf("a retired raise set the limit to %v when its collection completed, want nothing", set)
		}
		if raise.armed.Load() {
			test.Error("a retired raise armed again")
		}
	})

	// Not parallel: it counts collections, as its parent does. Retired before its load ends, a raise stops arming, so
	// no sentinel of its outlives the load that retired it.
	test.Run("a raise retired before its load's end stops arming", func(test *testing.T) {
		setter := &limitSetter{}
		raise := newLateRaise(limit, setter.setMemoryLimit, notCollecting)

		raise.retire()
		runtime.GC()
		waitFor(test, "the sentinel's finalizer", func() bool { return raise.collections.Load() >= 1 })

		if raise.armed.Load() {
			test.Error("a retired raise armed again before its load's end")
		}
		raise.endLoad()
		if set := setter.limits(); len(set) > 0 {
			test.Errorf("a retired raise set the limit to %v at its load's end, want nothing", set)
		}
	})

	// Not parallel: it counts collections, as its parent does.
	test.Run("no collection is marking once runtime.GC returns", func(test *testing.T) {
		runtime.GC()

		if collectorMarking() {
			test.Error("collectorMarking read a collection in flight after runtime.GC returned")
		}
	})
}
