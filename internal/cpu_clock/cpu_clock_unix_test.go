//go:build darwin || linux

package cpu_clock

import (
	"sync/atomic"
	"testing"
	"time"
)

// spinSink keeps the spin's arithmetic from being optimized away.
var spinSink atomic.Uint64

// spinUntilUser does arithmetic alone, in user space, until the process's user CPU has grown by user, or deadline has
// passed, and reports whether it grew, reading only the clock it measures, the process's, so a machine that gives the
// process little of a core makes it spin longer and never fail. The arithmetic runs in batches between reads, so the
// reads' own system time is a small part of the spin.
func spinUntilUser(test *testing.T, user time.Duration, deadline time.Duration) bool {
	test.Helper()
	before, _, known := ProcessUserSystem()
	if !known {
		test.Fatal("the process's CPU could not be read")
	}
	value := uint64(1)
	for started := time.Now(); time.Since(started) < deadline; {
		for range 1_000_000 {
			value = value*6364136223846793005 + 1442695040888963407
		}
		if now, _, _ := ProcessUserSystem(); now-before >= user {
			spinSink.Add(value)
			return true
		}
	}
	spinSink.Add(value)
	return false
}

// ProcessUserSystem keeps user and system apart: a stretch of pure arithmetic, run until the process's user CPU has
// grown by 30ms, adds more to its user CPU than to its system CPU, so a clock that swapped the two reads red here, and
// one that summed them into one never reaches more user than system.
func TestProcessUserSystemCountsUserWorkAsUser(test *testing.T) {
	test.Parallel()
	userBefore, systemBefore, known := ProcessUserSystem()
	if !known {
		test.Fatal("the process's CPU could not be read")
	}
	if !spinUntilUser(test, 30*time.Millisecond, 10*time.Second) {
		test.Fatal("10s of arithmetic never added 30ms to the process's user CPU, so the clock reads user work as " +
			"something else, or the process was given no core to run on")
	}
	userAfter, systemAfter, known := ProcessUserSystem()
	if !known {
		test.Fatal("the process's CPU could not be read")
	}
	if user, system := userAfter-userBefore, systemAfter-systemBefore; user <= system {
		test.Errorf("arithmetic added %v of user CPU and %v of system, want more user than system", user, system)
	}
	if total, totalKnown := Process(); !totalKnown || total < userAfter+systemAfter {
		test.Errorf("the process's CPU reads %v, less than its user and system so far, %v and %v", total, userAfter,
			systemAfter)
	}
}
