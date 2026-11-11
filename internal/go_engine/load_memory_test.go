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

// A run with extra builds loads many times, the host's build, each extra build's and their reloads, and each load's
// raise lands at the same cap plus one allowance, never on the last raise's limit: every load runs under the cap and
// no limit the run ever sets passes the cap plus lateMemoryAllowance, the 9 GiB @system_cohere ruled the run's worst
// case (#44n0vap, #b2t9xmf), however many loads there are.
func TestManyLoadsNeverRaiseTheLimitPastOneAllowance(test *testing.T) {
	test.Parallel()
	watch := &memoryWatch{current: engineMemoryCap}
	loads := newLoadMemory(capMemoryWatched(watch, watch.collecting), false)
	ceiling := engineMemoryCap + lateMemoryAllowance

	const loadCount = 8
	for range loadCount {
		loads.begin()
		watch.lock.Lock()
		during := watch.current
		watch.lock.Unlock()
		if during != engineMemoryCap {
			test.Errorf("a load ran under a limit of %d, want the cap %d", during, engineMemoryCap)
		}
		loads.end()
	}
	for _, limit := range watch.setLimits() {
		if limit > ceiling {
			test.Errorf("the run set its limit to %d, past the cap plus one allowance, %d", limit, ceiling)
		}
	}
	raised := loads.raisedTo()
	if len(raised) != loadCount {
		test.Fatalf("%d loads landed %d raises, want one each", loadCount, len(raised))
	}
	for _, limit := range raised {
		if limit != ceiling {
			test.Errorf("a raise landed at %d, want every raise at the cap plus one allowance, %d", limit, ceiling)
		}
	}
}
