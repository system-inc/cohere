package main

import (
	"fmt"
	"math"
	"runtime/debug"
)

// memoryPolicy is what this process decided about its collector, kept so the summary can say so.
type memoryPolicy struct {
	// environment names the variables that set the collector, when any did, and then nothing else applies.
	environment string
	// available is the memory free for this run when it started, ceiling the limit set from it.
	available uint64
	ceiling   uint64
	// unreadable is why available memory could not be read, which leaves the runtime's defaults.
	unreadable error
	// testBinary is a `go test` process, which keeps the runtime's defaults.
	testBinary bool
}

func (policy memoryPolicy) apply() {
	if policy.ceiling == 0 {
		return
	}
	// The limit first: with collection off and no limit, nothing would bound the heap for the instant
	// between the two calls.
	debug.SetMemoryLimit(int64(min(policy.ceiling, math.MaxInt64)))
	debug.SetGCPercent(-1)
}

// line is the summary's account of the collector, so a run's memory use is never a surprise.
func (policy memoryPolicy) line() string {
	switch {
	case policy.testBinary:
		return "memory: the collector runs at Go's default, since this is a test binary"
	case policy.environment != "":
		return "memory: the environment sets the collector (" + policy.environment + ")"
	case policy.unreadable != nil:
		return fmt.Sprintf(
			"memory: the collector runs at Go's default, since available memory could not be read (%v)",
			policy.unreadable,
		)
	}
	return fmt.Sprintf(
		"memory: collector off, ceiling %s (half of %s available)",
		gigabytes(policy.ceiling),
		gigabytes(policy.available),
	)
}
