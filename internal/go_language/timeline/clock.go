package timeline

import (
	"time"

	"github.com/system-inc/cohere/internal/cpu_clock"
)

// Clock is where a recorder reads the time: the wall clock, and the process's user and system CPU so far, known false
// where the CPU cannot be read. Each is a function so a test can hand a recorder a clock that counts its reads, and
// read the count to see that a run that did not ask read nothing.
type Clock struct {
	Now          func() time.Time
	ProcessTimes func() (user time.Duration, system time.Duration, known bool)
}

// SystemClock is the clock a run reads: the wall clock, and the kernel's count of the process's CPU
// (cpu_clock.ProcessUserSystem).
func SystemClock() Clock {
	return Clock{Now: time.Now, ProcessTimes: cpu_clock.ProcessUserSystem}
}
