// Package dotted imports time into its own scope alone, so time names nothing here.
package dotted

import . "time"

// dotted calls time.After as After.
func dotted() {
	// The original's time.Sleep(Second) names nothing and does not compile, and the port writes Sleep(Second), which
	// the dot import brings into the file's scope.
	select { // port moves: should use time.Sleep instead of elaborate way of sleeping
	case <-After(Second):
	}
}

// shadowedSleep declares a Sleep of its own over the dot import's.
func shadowedSleep() {
	Sleep := func(Duration) {}
	Sleep(0)
	// Sleep here is the function above and the file names time by no other name, so no spelling the port could write
	// calls time.Sleep, and the original's time.Sleep(0) names nothing. The port keeps the finding.
	select { // port withholds: Use time.Sleep
	case <-After(0):
	}
}
