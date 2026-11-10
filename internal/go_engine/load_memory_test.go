package go_engine

import (
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/contract"
)

// capMemoryWatched is a decision under the cap with room above it, whose limit and collector reads watch keeps.
func capMemoryWatched(watch *memoryWatch, collecting func() bool) *engineMemory {
	return &engineMemory{
		record: contract.Memory{
			LimitBytes: engineMemoryCap, CapBytes: engineMemoryCap, Source: contract.MemorySourceCap,
		},
		inheritedBytes: 32 << 30,
		setMemoryLimit: watch.setMemoryLimit,
		collecting:     collecting,
	}
}

func TestALoadMemoryWithoutADecisionDoesNothing(test *testing.T) {
	test.Parallel()

	loads := newLoadMemory(nil, false)
	loads.begin()
	loads.end()

	if loads != nil || loads.raisedTo() != nil {
		test.Error("a run without a memory decision made a load memory")
	}
}

// Two loads in flight at once share one cap and one raise: only the first to begin resets the limit and arms, and
// only the last to end lets the raise land.
func TestLoadsInFlightShareOneRaise(test *testing.T) {
	test.Parallel()
	watch := &memoryWatch{current: engineMemoryCap}
	loads := newLoadMemory(capMemoryWatched(watch, watch.collecting), false)
	raised := engineMemoryCap + lateMemoryAllowance

	loads.begin()
	loads.begin()
	loads.end()

	if set := watch.setLimits(); !slices.Equal(set, []int64{engineMemoryCap}) {
		test.Fatalf("with a load still in flight the limit was set to %v, want only the cap at the first's start", set)
	}
	loads.end()
	if set := watch.setLimits(); !slices.Equal(set, []int64{engineMemoryCap, raised}) {
		test.Errorf("after the last load ended the limit was set to %v, want %v", set,
			[]int64{engineMemoryCap, raised})
	}
}

// raisedTo is the one place that knows the raises the run made: each that landed, in order.
func TestRaisedToNamesEveryRaiseThatLanded(test *testing.T) {
	test.Parallel()
	watch := &memoryWatch{current: engineMemoryCap}
	loads := newLoadMemory(capMemoryWatched(watch, watch.collecting), false)
	raised := engineMemoryCap + lateMemoryAllowance

	for range 2 {
		loads.begin()
		loads.end()
	}

	if limits := loads.raisedTo(); !slices.Equal(limits, []int64{raised, raised}) {
		test.Errorf("raisedTo is %v after two loads, want %v", limits, []int64{raised, raised})
	}
}
