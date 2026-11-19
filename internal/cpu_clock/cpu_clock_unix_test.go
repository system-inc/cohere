//go:build darwin || linux

package cpu_clock

import (
	"sync/atomic"
	"testing"
	"time"
)

// spinSink keeps the spin's arithmetic from being optimized away.
var spinSink atomic.Uint64

// spinFor does arithmetic alone, in user space, for wall, reading only the wall clock, which on darwin and linux is
// read without entering the kernel.
func spinFor(wall time.Duration) {
	value := uint64(1)
	for started := time.Now(); time.Since(started) < wall; {
		for range 10000 {
			value = value*6364136223846793005 + 1442695040888963407
		}
	}
	spinSink.Add(value)
}

// ProcessUserSystem keeps user and system apart: a stretch of pure arithmetic adds to the process's user CPU, at least
// half of its wall, and more than to its system CPU, so a clock that swapped the two, or summed them into one, reads
// red here.
func TestProcessUserSystemCountsUserWorkAsUser(test *testing.T) {
	test.Parallel()
	userBefore, systemBefore, known := ProcessUserSystem()
	if !known {
		test.Fatal("the process's CPU could not be read")
	}
	spinFor(60 * time.Millisecond)
	userAfter, systemAfter, known := ProcessUserSystem()
	if !known {
		test.Fatal("the process's CPU could not be read")
	}
	user, system := userAfter-userBefore, systemAfter-systemBefore
	if user < 30*time.Millisecond || user <= system {
		test.Errorf("60ms of arithmetic added %v of user CPU and %v of system, want at least 30ms of user and more "+
			"user than system", user, system)
	}
	if total, totalKnown := Process(); !totalKnown || total < userAfter+systemAfter {
		test.Errorf("the process's CPU reads %v, less than its user and system so far, %v and %v", total, userAfter,
			systemAfter)
	}
}
