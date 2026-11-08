// Package shadowed imports time under its own name and another, and declares a time of its own over the first.
package shadowed

import (
	"time"
	clock "time"
)

// Wait takes a parameter named time, so time names the string where the select stands.
func Wait(time string) time.Duration {
	// The original's time.Sleep(0) selects Sleep of the string parameter and does not compile, and the port writes
	// clock.Sleep(0), the name the call of After already uses.
	select { // port moves: should use time.Sleep instead of elaborate way of sleeping
	case <-clock.After(0):
	}
	println(time)
	return 0
}
