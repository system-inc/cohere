// Package dotted imports time into its own scope and under its own name both, so time names the import where each
// select stands, and the fix the original writes, time.Sleep, compiles.
package dotted

import (
	"time"
	. "time"
)

// waits calls time.After through the dot import, reported and fixed both ways.
func waits() {
	select { // reported, fixed
	case <-After(Second):
	}
	select { // reported, fixed
	case <-time.After(time.Second):
	}
}
