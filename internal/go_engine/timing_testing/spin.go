// Package timing_testing is for the Go engine's tests of `--timing` (#3apgjer), which plant an analyzer, a listener
// or a rule that spends a known amount of its own thread's CPU and read what the run billed it.
package timing_testing

import (
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/cpu_clock"
)

// spinStep is how many iterations Spin runs between two reads of the clock, a few microseconds of work, so the reads
// are a small part of what it spends.
const spinStep = 10_000

// spinSink keeps the compiler from deleting the spin as dead code.
var spinSink atomic.Uint64

// Spin spends at least cpu of the calling thread's CPU and returns what it spent, by the thread's own clock, read on a
// thread held for the spin, so time the goroutine spent descheduled is never counted as spent. It returns 0 at once
// where the clock cannot be read, which RequireThreadClock keeps a test from reaching.
func Spin(cpu time.Duration) time.Duration {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	start, known := cpu_clock.Thread()
	if !known {
		return 0
	}
	value := uint64(1)
	for {
		for range spinStep {
			value = value*6364136223846793005 + 1442695040888963407
		}
		now, _ := cpu_clock.Thread()
		if now-start >= cpu {
			spinSink.Add(value)
			return now - start
		}
	}
}

// RequireThreadClock fails test where the thread clock cannot be read, since a run there measures nothing and a
// timing test would prove nothing. It fails rather than skips, so a platform that lost its clock never reads as one
// whose timing passed.
func RequireThreadClock(test *testing.T) {
	test.Helper()
	if _, known := cpu_clock.Thread(); !known {
		test.Fatal("this platform has no per-thread CPU clock, so --timing measures nothing here")
	}
}
