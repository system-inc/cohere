package bounded

import (
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
)

// TestForEachRunsEveryIndexOnceAndNoMoreAtOnceThanThereAreCores runs work over many indexes and finds each run exactly
// once, with never more running at once than there are cores.
func TestForEachRunsEveryIndexOnceAndNoMoreAtOnceThanThereAreCores(test *testing.T) {
	test.Parallel()
	const count = 200
	runs := make([]atomic.Int64, count)
	var running atomic.Int64
	var mostAtOnce atomic.Int64
	ForEach(count, false, func(index int) {
		now := running.Add(1)
		defer running.Add(-1)
		for {
			most := mostAtOnce.Load()
			if now <= most || mostAtOnce.CompareAndSwap(most, now) {
				break
			}
		}
		runs[index].Add(1)
		runtime.Gosched()
	})
	for index := range runs {
		if ran := runs[index].Load(); ran != 1 {
			test.Errorf("index %d ran %d times, want once", index, ran)
		}
	}
	if most, cores := mostAtOnce.Load(), int64(runtime.GOMAXPROCS(0)); most > cores {
		test.Errorf("%d ran at once, more than the %d cores", most, cores)
	}
}

// TestForEachSingleThreadedRunsTheIndexesInOrderOneAtATime runs work single-threaded and finds the indexes run in
// order, each after the last has returned.
func TestForEachSingleThreadedRunsTheIndexesInOrderOneAtATime(test *testing.T) {
	test.Parallel()
	var lock sync.Mutex
	var order []int
	var running atomic.Int64
	ForEach(50, true, func(index int) {
		if running.Add(1) != 1 {
			test.Errorf("index %d ran beside another", index)
		}
		defer running.Add(-1)
		lock.Lock()
		order = append(order, index)
		lock.Unlock()
	})
	want := make([]int, 50)
	for index := range want {
		want[index] = index
	}
	if !slices.Equal(order, want) {
		test.Errorf("ran in the order %v, want %v", order, want)
	}
}
