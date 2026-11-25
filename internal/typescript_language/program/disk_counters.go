package program

import (
	"sync/atomic"
	"time"
)

// diskCounters accumulates calls from many goroutines at once. Atomic rather than locked, because a
// lock here would serialize the loaders and measure itself.
type diskCounters struct {
	counts    [diskCallKinds]atomic.Int64
	durations [diskCallKinds]atomic.Int64
}

func (counters *diskCounters) add(kind int, started time.Time) {
	counters.counts[kind].Add(1)
	counters.durations[kind].Add(int64(time.Since(started)))
}

func (counters *diskCounters) snapshot() DiskTiming {
	calls := func(kind int) DiskCalls {
		return DiskCalls{Count: counters.counts[kind].Load(), Summed: time.Duration(counters.durations[kind].Load())}
	}
	return DiskTiming{
		Reads:     calls(diskRead),
		Existence: calls(diskExistence),
		Stats:     calls(diskStat),
		Listings:  calls(diskListing),
		Realpaths: calls(diskRealpath),
	}
}
