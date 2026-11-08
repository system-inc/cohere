package shadowed

import (
	"time"
	clock "time"
)

// A parameter named time shadows the file's import of time, so the original's time.Since selects from a time.Time,
// which has no Since, and the port writes the name the call of time.Now uses, clock.Since.
func shadowed(time clock.Time) clock.Duration {
	// time names the parameter where the call stands, and clock the import.
	return clock.Now().Sub(time) // port moves: should use time.Since instead of time.Now().Sub
}

// Outside the parameter's function, time names the import, and the original's fix is right.
func unshadowed(start time.Time) time.Duration {
	return clock.Now().Sub(start)
}
